// Package events records audit log entries and delivers webhooks.
package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/p-arndt/doreg/internal/meta"
)

// Webhook event types. Audit actions use the same names where they overlap.
const (
	Push          = "push"
	Pull          = "pull"
	DeleteTag     = "delete_tag"
	DeleteImage   = "delete_manifest"
	DeleteRepo    = "delete_repository"
	QuotaExceeded = "quota_exceeded"
	GCCompleted   = "gc_completed"
)

// WebhookEvents lists the events a webhook can subscribe to.
var WebhookEvents = []string{Push, Pull, DeleteTag, DeleteImage, DeleteRepo, QuotaExceeded, GCCompleted}

// Event is something that happened.
type Event struct {
	Action     string `json:"type"`
	At         int64  `json:"occurredAt"`
	Actor      string `json:"operator"`
	ActorKind  string `json:"operatorKind"`
	Project    string `json:"project,omitempty"`
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Digest     string `json:"digest,omitempty"`
	MediaType  string `json:"mediaType,omitempty"`
	// ResourceURL is the pullable reference, e.g. reg.example.com/team/api:1.0.
	ResourceURL string `json:"resourceUrl,omitempty"`
	Detail      string `json:"detail,omitempty"`
	IP          string `json:"-"`
}

func (e Event) resource() string {
	switch {
	case e.Repository != "" && e.Tag != "":
		return e.Repository + ":" + e.Tag
	case e.Repository != "" && e.Digest != "":
		return e.Repository + "@" + e.Digest
	}
	return e.Repository
}

// Bus writes events to the audit log and queues webhook deliveries.
type Bus struct {
	db     *meta.DB
	log    *slog.Logger
	client *http.Client
	queue  chan delivery
	// Retry delays between attempts; tests shorten them.
	Backoff []time.Duration
	// AllowPrivate permits deliveries to loopback, private, link-local and
	// similar internal addresses. Off by default so project admins cannot use
	// webhooks to reach services inside the operator's network. Set from
	// DOREG_WEBHOOK_ALLOW_PRIVATE.
	AllowPrivate bool
}

// ErrDestinationNotAllowed is the dial error for a webhook destination that
// resolves to an internal address while AllowPrivate is off.
var ErrDestinationNotAllowed = errors.New("destination address is not allowed")

// blockedPrefixes are ranges not covered by the netip predicates in
// destinationAllowed.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // "this network"; 0.x reaches the local host on Linux
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, often used for internal overlays
}

func destinationAllowed(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// newClient checks the destination at dial time, after DNS resolution, so
// neither DNS rebinding nor a hostname pointing inward can bypass it.
func (b *Bus) newClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			if b.AllowPrivate {
				return nil
			}
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !destinationAllowed(ap.Addr()) {
				return ErrDestinationNotAllowed
			}
			return nil
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// An environment proxy would make the dial check see the proxy's address
	// instead of the webhook's.
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		// A redirect could point inward or to another host than the one the
		// admin configured; treat it as a failed delivery.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type delivery struct {
	hook    meta.Webhook
	event   string
	body    []byte
	attempt int // attempts made so far
}

// New starts a bus with background delivery workers.
func New(db *meta.DB, log *slog.Logger) *Bus {
	b := &Bus{
		db:           db,
		log:          log,
		queue:        make(chan delivery, 1000),
		Backoff:      []time.Duration{5 * time.Second, 30 * time.Second},
		AllowPrivate: os.Getenv("DOREG_WEBHOOK_ALLOW_PRIVATE") == "true",
	}
	b.client = b.newClient()
	for range 2 {
		go b.worker()
	}
	return b
}

// Emit records e and notifies subscribed webhooks. It never blocks the caller
// on network I/O.
func (b *Bus) Emit(ctx context.Context, e Event) {
	if e.At == 0 {
		e.At = meta.Now()
	}
	ctx = context.WithoutCancel(ctx)
	if err := b.db.AddAudit(ctx, meta.AuditEntry{
		At: e.At, Actor: e.Actor, ActorKind: e.ActorKind, Action: e.Action,
		Project: e.Project, Resource: e.resource(), Detail: e.Detail, IP: e.IP,
	}); err != nil {
		b.log.Error("audit write failed", "err", err)
	}
	hooks, err := b.db.WebhooksFor(ctx, e.Project, e.Action)
	if err != nil {
		b.log.Error("webhook lookup failed", "err", err)
		return
	}
	if len(hooks) == 0 {
		return
	}
	body, _ := json.Marshal(e)
	for _, h := range hooks {
		select {
		case b.queue <- delivery{hook: h, event: e.Action, body: body}:
		default:
			b.log.Warn("webhook queue full, dropping delivery", "webhook", h.ID, "event", e.Action)
		}
	}
}

// Audit records an administrative action that is not a webhook event.
func (b *Bus) Audit(ctx context.Context, actor, actorKind, action, project, resource, detail, ip string) {
	if err := b.db.AddAudit(context.WithoutCancel(ctx), meta.AuditEntry{
		At: meta.Now(), Actor: actor, ActorKind: actorKind, Action: action,
		Project: project, Resource: resource, Detail: detail, IP: ip,
	}); err != nil {
		b.log.Error("audit write failed", "err", err)
	}
}

// Test sends a synthetic event to one webhook and waits for the outcome.
func (b *Bus) Test(ctx context.Context, h meta.Webhook, actor string) meta.Delivery {
	body, _ := json.Marshal(Event{Action: "ping", At: meta.Now(), Actor: actor, ActorKind: "user", Detail: "test delivery from doreg"})
	return b.send(ctx, h, "ping", body, 1)
}

func (b *Bus) worker() {
	for d := range b.queue {
		d.attempt++
		res := b.send(context.Background(), d.hook, d.event, d.body, d.attempt)
		if res.Error == "" && res.StatusCode < 300 {
			continue
		}
		if d.attempt > len(b.Backoff) {
			b.log.Warn("webhook delivery failed", "webhook", d.hook.ID, "event", d.event, "status", res.StatusCode, "err", res.Error)
			continue
		}
		// Re-queue after the backoff instead of sleeping, so one failing
		// endpoint does not hold up deliveries to the others.
		time.AfterFunc(b.Backoff[d.attempt-1], func() {
			select {
			case b.queue <- d:
			default:
				b.log.Warn("webhook queue full, dropping retry", "webhook", d.hook.ID, "event", d.event)
			}
		})
	}
}

// Sign returns the signature header value for body.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func (b *Bus) send(ctx context.Context, h meta.Webhook, event string, body []byte, attempt int) meta.Delivery {
	start := time.Now()
	res := meta.Delivery{WebhookID: h.ID, Event: event, At: meta.Now(), Attempt: attempt, Payload: string(body)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "doreg-webhook")
		req.Header.Set("X-Doreg-Event", event)
		req.Header.Set("X-Doreg-Attempt", strconv.Itoa(attempt))
		if h.Secret != "" {
			req.Header.Set("X-Doreg-Signature-256", Sign(h.Secret, body))
		}
		var resp *http.Response
		resp, err = b.client.Do(req)
		if err == nil {
			res.StatusCode = resp.StatusCode
			// Drain so the connection can be reused.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		}
	}
	switch {
	case errors.Is(err, ErrDestinationNotAllowed):
		res.Error = ErrDestinationNotAllowed.Error()
	case err != nil:
		res.Error = err.Error()
	}
	res.DurationMs = time.Since(start).Milliseconds()
	if err := b.db.AddDelivery(context.WithoutCancel(ctx), res); err != nil {
		b.log.Error("delivery record failed", "err", err)
	}
	return res
}
