// Command doreg is a container registry with a web UI and online garbage collection.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/p-arndt/doreg/internal/api"
	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/buildinfo"
	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/gc"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oidc"
	"github.com/p-arndt/doreg/internal/registry"
	"github.com/p-arndt/doreg/internal/storage"
	"github.com/p-arndt/doreg/internal/webui"
)

const usage = `doreg — container registry with web UI and online garbage collection

Usage:
  doreg serve  [--data DIR] [--addr :8080]
  doreg user   add|set|delete|list [--data DIR] [--role admin|user] NAME
               (password is read from stdin)
  doreg gc     [--data DIR] [--dry-run] [--retention]
  doreg login  [--docker-host HOST] [--no-browser] [--print] REGISTRY
               (SSO login in the browser, then docker login)
  doreg import --from URL --to URL [--from-user U] [--to-user U] [--repo PATTERN]...
               [--from-insecure] [--concurrency N] [--dry-run]
               (copy repositories from another registry, e.g. registry:2;
               passwords in DOREG_FROM_PASSWORD and DOREG_TO_PASSWORD)
  doreg version

Environment:
  DOREG_DATA            data directory (default ./data)
  DOREG_ADDR            listen address (default :8080)
  DOREG_ADMIN_USER      with DOREG_ADMIN_PASSWORD: create this admin on start if no users exist
  DOREG_ADMIN_PASSWORD
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "user":
		err = user(os.Args[2:])
	case "gc":
		err = runGC(os.Args[2:])
	case "login":
		err = login(os.Args[2:])
	case "import":
		err = runImport(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("doreg %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type app struct {
	db    *meta.DB
	store *storage.FS
	log   *slog.Logger
}

func open(dataDir string) (*app, error) {
	store, err := storage.Open(dataDir)
	if err != nil {
		return nil, err
	}
	db, err := meta.Open(env("DATABASE_URL", filepath.Join(dataDir, "doreg.db")))
	if err != nil {
		return nil, err
	}
	return &app{db: db, store: store, log: slog.New(slog.NewTextHandler(os.Stderr, nil))}, nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	data := fs.String("data", env("DOREG_DATA", "./data"), "data directory")
	addr := fs.String("addr", env("DOREG_ADDR", ":8080"), "listen address")
	fs.Parse(args)

	a, err := open(*data)
	if err != nil {
		return err
	}
	defer a.db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bootstrapAdmin(ctx, a); err != nil {
		return err
	}
	authn := auth.New(a.db)
	bus := events.New(a.db, a.log)
	collector := gc.New(a.db, a.store, a.log)
	collector.OnFinish = func(ctx context.Context, run *gc.Run) {
		bus.Emit(ctx, events.Event{
			Action: events.GCCompleted, Actor: "system", ActorKind: "system",
			Detail: fmt.Sprintf("%s: freed %d bytes, %d manifests, %d blobs %s", run.Status, run.FreedBytes,
				run.DeletedManifests, run.DeletedBlobs, run.Error),
		})
	}
	go collector.Schedule(ctx)

	var provider *oidc.Provider
	cfg, err := oidc.ConfigFromEnv()
	if err != nil {
		return err
	}
	if cfg != nil {
		if provider, err = oidc.New(ctx, *cfg); err != nil {
			return err
		}
		a.log.Info("single sign-on enabled", "issuer", cfg.Issuer)
	}

	mux := http.NewServeMux()
	reg := &registry.Handler{DB: a.db, Store: a.store, Auth: authn, Events: bus, Log: a.log}
	mux.Handle("/v2/", reg)
	mux.Handle("/v2", http.RedirectHandler("/v2/", http.StatusMovedPermanently))
	apiHandler := api.New(a.db, a.store, authn, collector, bus, provider, a.log)
	apiHandler.Registry = reg
	apiHandler.PublicBrowse = os.Getenv("DOREG_PUBLIC_BROWSE") == "true"
	mux.Handle("/api/", apiHandler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("/", webui.Handler())

	srv := &http.Server{
		Addr:    *addr,
		Handler: mux,
		// No Read/WriteTimeout: layer pushes and pulls can take many minutes.
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	a.log.Info("doreg listening", "addr", *addr, "data", *data, "version", buildinfo.Version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func bootstrapAdmin(ctx context.Context, a *app) error {
	pw := os.Getenv("DOREG_ADMIN_PASSWORD")
	if pw == "" {
		return nil
	}
	users, err := a.db.Users(ctx)
	if err != nil || len(users) > 0 {
		return err
	}
	name := env("DOREG_ADMIN_USER", "admin")
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	a.log.Info("created initial admin user", "user", name)
	return a.db.UpsertUser(ctx, name, hash, meta.RoleAdmin)
}

func readPassword() (string, error) {
	if fi, _ := os.Stdin.Stat(); fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprint(os.Stderr, "Password: ")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func user(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: doreg user add|set|delete|list NAME")
	}
	cmd := args[0]
	fs := flag.NewFlagSet("user", flag.ExitOnError)
	data := fs.String("data", env("DOREG_DATA", "./data"), "data directory")
	role := fs.String("role", meta.RoleUser, "role: admin or user")
	fs.Parse(args[1:])
	a, err := open(*data)
	if err != nil {
		return err
	}
	defer a.db.Close()
	ctx := context.Background()

	if cmd == "list" {
		users, err := a.db.Users(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			fmt.Printf("%-24s %s\n", u.Username, u.Role)
		}
		return nil
	}
	if fs.NArg() != 1 {
		return errors.New("missing user name")
	}
	name := fs.Arg(0)
	switch cmd {
	case "add", "set":
		if !meta.ValidRole(*role) {
			return fmt.Errorf("invalid role %q", *role)
		}
		pw, err := readPassword()
		if err != nil {
			return err
		}
		if len(pw) < 8 {
			return errors.New("password must be at least 8 characters")
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		return a.db.UpsertUser(ctx, name, hash, *role)
	case "delete":
		u, err := a.db.UserByName(ctx, name)
		if err != nil {
			return err
		}
		return a.db.DeleteUser(ctx, u.ID)
	}
	return fmt.Errorf("unknown user command %q", cmd)
}

func runGC(args []string) error {
	fs := flag.NewFlagSet("gc", flag.ExitOnError)
	data := fs.String("data", env("DOREG_DATA", "./data"), "data directory")
	dry := fs.Bool("dry-run", false, "only report what would be deleted")
	retention := fs.Bool("retention", false, "apply retention policies")
	fs.Parse(args)
	a, err := open(*data)
	if err != nil {
		return err
	}
	defer a.db.Close()
	run, err := gc.New(a.db, a.store, a.log).Run(context.Background(), gc.Options{DryRun: *dry, ApplyRetention: *retention, Trigger: "cli"})
	if run != nil && run.Report != nil {
		r := run.Report
		fmt.Printf("tags: %d  manifests: %d  blobs: %d  orphan files: %d  stale uploads: %d  freed: %d bytes\n",
			len(r.DeletedTags), len(r.DeletedManifests), r.DeletedBlobs, r.OrphanFiles, r.StaleUploads, r.FreedBytes)
	}
	return err
}
