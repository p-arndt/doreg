package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/p-arndt/doreg/internal/importer"
	"github.com/p-arndt/doreg/internal/registry"
)

// repeated collects a flag that may be given several times.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

func runImport(args []string) error {
	return importCmd(context.Background(), args, os.Stdout, os.Getenv)
}

// importCmd copies repositories from another registry into a running doreg
// over the registry API. Passwords come from the environment so they stay out
// of the shell history and the process list.
func importCmd(ctx context.Context, args []string, out io.Writer, getenv func(string) string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(out)
	from := fs.String("from", "", "source registry URL, e.g. https://old-registry:5000")
	fromUser := fs.String("from-user", "", "user for the source registry (password in DOREG_FROM_PASSWORD)")
	fromInsecure := fs.Bool("from-insecure", false, "do not verify the source registry's TLS certificate")
	to := fs.String("to", "", "doreg URL, e.g. https://registry.example.com")
	toUser := fs.String("to-user", "", "doreg user, ideally an admin (password or CLI secret in DOREG_TO_PASSWORD)")
	var repos repeated
	fs.Var(&repos, "repo", "repository or pattern to import (repeatable, e.g. 'team/*'); default all")
	concurrency := fs.Int("concurrency", 4, "tags copied in parallel")
	dryRun := fs.Bool("dry-run", false, "only list what would be imported")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return errors.New("usage: doreg import --from URL --to URL [--from-user U] [--to-user U] [--repo PATTERN]... [--dry-run]")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var srcTransport http.RoundTripper
	if *fromInsecure {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		srcTransport = t
	}
	src, err := importer.NewClient(*from, importer.Credentials{Username: *fromUser, Password: getenv("DOREG_FROM_PASSWORD")}, srcTransport, nil)
	if err != nil {
		return err
	}
	dst, err := importer.NewClient(*to, importer.Credentials{Username: *toUser, Password: getenv("DOREG_TO_PASSWORD")}, nil,
		http.Header{registry.ImportHeader: {"1"}})
	if err != nil {
		return err
	}
	if err := src.Ping(ctx); err != nil {
		return fmt.Errorf("source registry: %w", err)
	}
	if err := dst.Ping(ctx); err != nil {
		return fmt.Errorf("doreg: %w", err)
	}

	im := &importer.Importer{Src: src, Dst: dst, Concurrency: *concurrency}
	plan, err := im.Plan(ctx, repos)
	if err != nil {
		return err
	}
	tags := 0
	for _, r := range plan {
		tags += len(r.Tags)
	}
	if *dryRun {
		for _, r := range plan {
			fmt.Fprintf(out, "%-50s %d tags\n", r.Name, len(r.Tags))
		}
		fmt.Fprintf(out, "%d repositories, %d tags\n", len(plan), tags)
		return nil
	}
	fmt.Fprintf(out, "importing %d repositories, %d tags from %s\n", len(plan), tags, src.Host())
	im.OnTag = func(repo, tag, outcome string, err error) {
		if err != nil {
			fmt.Fprintf(out, "%-8s %s:%s: %v\n", outcome, repo, tag, err)
			return
		}
		fmt.Fprintf(out, "%-8s %s:%s\n", outcome, repo, tag)
	}
	p, err := im.Run(ctx, plan)
	fmt.Fprintf(out, "%d copied, %d up to date, %d failed, %d blobs, %d bytes\n",
		p.TagsCopied, p.TagsSkipped, p.TagsFailed, p.Blobs, p.Bytes)
	if err != nil {
		return err
	}
	if p.TagsFailed > 0 {
		return fmt.Errorf("%d tags failed; run the import again to retry them", p.TagsFailed)
	}
	return nil
}
