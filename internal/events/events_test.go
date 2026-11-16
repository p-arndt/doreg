package events_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/p-arndt/doreg/internal/events"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
)

const testSecret = "s3cret"

type hit struct {
	event     string
	attempt   string
	signature string
	body      []byte
}

// receiver records every request by URL path and fails paths starting with /fail.
type receiver struct {
	mu   sync.Mutex
	srv  *httptest.Server
	hits map[string][]hit
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{hits: map[string][]hit{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.hits[req.URL.Path] = append(r.hits[req.URL.Path], hit{
			event:     req.Header.Get("X-Doreg-Event"),
			attempt:   req.Header.Get("X-Doreg-Attempt"),
			signature: req.Header.Get("X-Doreg-Signature-256"),
			body:      body,
		})
		r.mu.Unlock()
		if req.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hits[path])
}

func (r *receiver) get(path string) []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hit(nil), r.hits[path]...)
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func setup(t *testing.T) (*meta.DB, *events.Bus) {
	t.Helper()
	db, err := meta.Open(metatest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	bus := events.New(db, slog.New(slog.DiscardHandler))
	bus.Backoff = []time.Duration{time.Millisecond, time.Millisecond}
	// The receivers are httptest servers on 127.0.0.1.
	bus.AllowPrivate = true
	return db, bus
}

func addHook(t *testing.T, db *meta.DB, w meta.Webhook) int64 {
	t.Helper()
	id, err := db.SaveWebhook(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDeliverySignedWithEventHeader(t *testing.T) {
	db, bus := setup(t)
	rcv := newReceiver(t)
	addHook(t, db, meta.Webhook{Name: "system", URL: rcv.srv.URL + "/system", Secret: testSecret,
		Events: []string{events.Push}, Enabled: true})

	bus.Emit(context.Background(), events.Event{Action: events.Push, Actor: "alice", ActorKind: "user",
		Repository: "app/api", Tag: "1.0"})

	waitFor(t, "delivery", func() bool { return rcv.count("/system") == 1 })
	h := rcv.get("/system")[0]
	if h.event != events.Push {
		t.Errorf("X-Doreg-Event = %q, want %q", h.event, events.Push)
	}
	if h.attempt != "1" {
		t.Errorf("X-Doreg-Attempt = %q, want 1", h.attempt)
	}
	if want := events.Sign(testSecret, h.body); h.signature != want {
		t.Errorf("signature = %q, want %q", h.signature, want)
	}
	if h.signature == events.Sign("wrong", h.body) {
		t.Error("signature verifies with the wrong secret")
	}
}

func TestProjectHookReceivesOnlyItsProject(t *testing.T) {
	db, bus := setup(t)
	rcv := newReceiver(t)
	ctx := context.Background()
	team, err := db.CreateProject(ctx, meta.Project{Name: "team"})
	if err != nil {
		t.Fatal(err)
	}
	addHook(t, db, meta.Webhook{Name: "system", URL: rcv.srv.URL + "/system", Events: []string{events.Push}, Enabled: true})
	addHook(t, db, meta.Webhook{Name: "team", URL: rcv.srv.URL + "/team", ProjectID: team.ID,
		Events: []string{events.Push}, Enabled: true})

	bus.Emit(ctx, events.Event{Action: events.Push, Project: "team", Repository: "team/api", Tag: "1"})
	bus.Emit(ctx, events.Event{Action: events.Push, Project: "other", Repository: "other/api", Tag: "1"})

	waitFor(t, "system hook to receive both events", func() bool { return rcv.count("/system") == 2 })
	waitFor(t, "team hook to receive its event", func() bool { return rcv.count("/team") == 1 })
	if n := rcv.count("/team"); n != 1 {
		t.Errorf("team hook received %d events, want 1", n)
	}
}

func TestUnsubscribedHookReceivesNothing(t *testing.T) {
	db, bus := setup(t)
	rcv := newReceiver(t)
	ctx := context.Background()
	addHook(t, db, meta.Webhook{Name: "deletes", URL: rcv.srv.URL + "/deletes", Events: []string{events.DeleteTag}, Enabled: true})
	addHook(t, db, meta.Webhook{Name: "pushes", URL: rcv.srv.URL + "/pushes", Events: []string{events.Push}, Enabled: true})

	// The sentinel tells us when the bus has finished handling the push.
	bus.Emit(ctx, events.Event{Action: events.Push, Repository: "app/api", Tag: "1"})
	bus.Emit(ctx, events.Event{Action: events.DeleteTag, Repository: "app/api", Tag: "1"})

	waitFor(t, "subscribed hook", func() bool { return rcv.count("/deletes") == 1 })
	waitFor(t, "push hook", func() bool { return rcv.count("/pushes") == 1 })
	if n := rcv.count("/deletes"); n != 1 {
		t.Errorf("delete hook received %d events, want 1 (the delete only)", n)
	}
	if got := rcv.get("/pushes")[0].event; got != events.Push {
		t.Errorf("push hook received %q", got)
	}
}

func TestDisabledHookReceivesNothing(t *testing.T) {
	db, bus := setup(t)
	rcv := newReceiver(t)
	ctx := context.Background()
	addHook(t, db, meta.Webhook{Name: "disabled", URL: rcv.srv.URL + "/disabled", Events: []string{events.Push}, Enabled: false})
	addHook(t, db, meta.Webhook{Name: "enabled", URL: rcv.srv.URL + "/enabled", Events: []string{events.Push}, Enabled: true})

	bus.Emit(ctx, events.Event{Action: events.Push, Repository: "app/api", Tag: "1"})

	waitFor(t, "enabled hook", func() bool { return rcv.count("/enabled") == 1 })
	if n := rcv.count("/disabled"); n != 0 {
		t.Errorf("disabled hook received %d events, want 0", n)
	}
}

func TestFailedDeliveryRetriedAndRecorded(t *testing.T) {
	db, bus := setup(t)
	rcv := newReceiver(t)
	ctx := context.Background()
	id := addHook(t, db, meta.Webhook{Name: "flaky", URL: rcv.srv.URL + "/fail", Events: []string{events.Push}, Enabled: true})

	bus.Emit(ctx, events.Event{Action: events.Push, Repository: "app/api", Tag: "1"})

	want := len(bus.Backoff) + 1
	waitFor(t, "all attempts recorded", func() bool {
		ds, err := db.Deliveries(ctx, id)
		return err == nil && len(ds) == want
	})
	if n := rcv.count("/fail"); n != want {
		t.Errorf("receiver got %d attempts, want %d", n, want)
	}
	ds, err := db.Deliveries(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, d := range ds {
		seen[d.Attempt] = true
		if d.StatusCode != http.StatusInternalServerError {
			t.Errorf("attempt %d status = %d, want 500", d.Attempt, d.StatusCode)
		}
		if d.Error == "" {
			t.Errorf("attempt %d recorded without an error", d.Attempt)
		}
	}
	for i := 1; i <= want; i++ {
		if !seen[i] {
			t.Errorf("attempt %d missing from deliveries", i)
		}
	}
}

func TestEmitWritesAuditRow(t *testing.T) {
	db, bus := setup(t)
	ctx := context.Background()

	bus.Emit(ctx, events.Event{Action: events.DeleteTag, Actor: "alice", ActorKind: "user", Project: "app",
		Repository: "app/api", Tag: "1.0", Detail: "manual"})

	rows, err := db.Audit(ctx, meta.AuditQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Action != events.DeleteTag || r.Actor != "alice" || r.Project != "app" ||
		r.Resource != "app/api:1.0" || r.Detail != "manual" || r.At == 0 {
		t.Errorf("unexpected audit row: %+v", r)
	}
}

func TestWithoutWebhooksWritesAuditOnly(t *testing.T) {
	db, bus := setup(t)
	rcv := newReceiver(t)
	ctx := context.Background()
	addHook(t, db, meta.Webhook{Name: "system", URL: rcv.srv.URL + "/system", Events: []string{events.Push}, Enabled: true})

	bus.Emit(events.WithoutWebhooks(ctx), events.Event{Action: events.Push, Repository: "app/api", Tag: "imported"})
	bus.Emit(ctx, events.Event{Action: events.Push, Repository: "app/api", Tag: "pushed"})

	// Deliveries are processed in order, so once the second arrived the first
	// would have too.
	waitFor(t, "delivery of the regular event", func() bool { return rcv.count("/system") >= 1 })
	hits := rcv.get("/system")
	if len(hits) != 1 || !strings.Contains(string(hits[0].body), `"tag":"pushed"`) {
		t.Fatalf("webhook received %d deliveries, want only the regular event", len(hits))
	}
	rows, err := db.Audit(ctx, meta.AuditQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("audit rows = %d, want 2 (quiet events are still audited)", len(rows))
	}
}

func TestLoopbackDestinationRefusedByDefault(t *testing.T) {
	db, bus := setup(t)
	bus.AllowPrivate = false
	rcv := newReceiver(t)
	ctx := context.Background()
	id := addHook(t, db, meta.Webhook{Name: "internal", URL: rcv.srv.URL + "/internal", Events: []string{events.Push}, Enabled: true})

	d := bus.Test(ctx, meta.Webhook{ID: id, URL: rcv.srv.URL + "/internal"}, "alice")
	if d.Error != events.ErrDestinationNotAllowed.Error() {
		t.Errorf("error = %q, want %q", d.Error, events.ErrDestinationNotAllowed.Error())
	}
	if d.StatusCode != 0 {
		t.Errorf("status = %d, want 0", d.StatusCode)
	}
	if n := rcv.count("/internal"); n != 0 {
		t.Errorf("receiver got %d requests, want 0", n)
	}
	ds, err := db.Deliveries(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Error != events.ErrDestinationNotAllowed.Error() {
		t.Errorf("deliveries = %+v, want one refused delivery", ds)
	}
}

func TestHostnameResolvingToLoopbackRefused(t *testing.T) {
	_, bus := setup(t)
	bus.AllowPrivate = false
	rcv := newReceiver(t)
	_, port, _ := net.SplitHostPort(rcv.srv.Listener.Addr().String())

	d := bus.Test(context.Background(), meta.Webhook{URL: "http://localhost:" + port + "/internal"}, "alice")
	if d.Error != events.ErrDestinationNotAllowed.Error() {
		t.Errorf("error = %q, want %q", d.Error, events.ErrDestinationNotAllowed.Error())
	}
	if n := rcv.count("/internal"); n != 0 {
		t.Errorf("receiver got %d requests, want 0", n)
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	_, bus := setup(t)
	rcv := newReceiver(t)
	redirector := httptest.NewServer(http.RedirectHandler(rcv.srv.URL+"/target", http.StatusFound))
	t.Cleanup(redirector.Close)

	d := bus.Test(context.Background(), meta.Webhook{URL: redirector.URL}, "alice")
	if d.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", d.StatusCode)
	}
	if d.Error == "" {
		t.Error("redirect recorded as a successful delivery")
	}
	if n := rcv.count("/target"); n != 0 {
		t.Errorf("redirect target got %d requests, want 0", n)
	}
}

func TestAllowPrivateDeliversToLoopback(t *testing.T) {
	_, bus := setup(t)
	rcv := newReceiver(t)

	d := bus.Test(context.Background(), meta.Webhook{URL: rcv.srv.URL + "/internal"}, "alice")
	if d.Error != "" || d.StatusCode != http.StatusOK {
		t.Errorf("delivery = status %d error %q, want 200", d.StatusCode, d.Error)
	}
	if n := rcv.count("/internal"); n != 1 {
		t.Errorf("receiver got %d requests, want 1", n)
	}
}

func TestAllowPrivateReadFromEnvironment(t *testing.T) {
	db, err := meta.Open(metatest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	t.Setenv("DOREG_WEBHOOK_ALLOW_PRIVATE", "true")
	if !events.New(db, slog.New(slog.DiscardHandler)).AllowPrivate {
		t.Error("AllowPrivate is false with DOREG_WEBHOOK_ALLOW_PRIVATE=true")
	}
	t.Setenv("DOREG_WEBHOOK_ALLOW_PRIVATE", "")
	if events.New(db, slog.New(slog.DiscardHandler)).AllowPrivate {
		t.Error("AllowPrivate is true without DOREG_WEBHOOK_ALLOW_PRIVATE")
	}
}
