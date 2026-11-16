package gc

import (
	"context"
	"log/slog"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/meta/metatest"
	"github.com/p-arndt/doreg/internal/storage"
)

const day = 86400

// fixClock pins meta.Now to now for the duration of the test.
func fixClock(t *testing.T, now int64) {
	t.Helper()
	orig := meta.Now
	meta.Now = func() int64 { return now }
	t.Cleanup(func() { meta.Now = orig })
}

func newCollector(t *testing.T) (*Collector, *meta.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := meta.Open(metatest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(db, store, slog.New(slog.DiscardHandler)), db
}

// seedAudit adds one old (200 days) and one recent (1 day) audit row.
func seedAudit(t *testing.T, db *meta.DB, now int64) {
	t.Helper()
	ctx := context.Background()
	for _, at := range []int64{now - 200*day, now - day} {
		if err := db.AddAudit(ctx, meta.AuditEntry{At: at, Actor: "alice", ActorKind: "user", Action: "push"}); err != nil {
			t.Fatal(err)
		}
	}
}

func auditCount(t *testing.T, db *meta.DB) int {
	t.Helper()
	rows, err := db.Audit(context.Background(), meta.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func TestRealRunPrunesOldAudit(t *testing.T) {
	const now = 1_800_000_000
	fixClock(t, now)
	c, db := newCollector(t)
	seedAudit(t, db, now)

	run, err := c.Run(context.Background(), Options{Trigger: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Report.PrunedAuditEntries != 1 {
		t.Errorf("PrunedAuditEntries = %d, want 1", run.Report.PrunedAuditEntries)
	}
	if n := auditCount(t, db); n != 1 {
		t.Errorf("audit rows after run = %d, want 1 (the recent one)", n)
	}
}

func TestDryRunKeepsOldAudit(t *testing.T) {
	const now = 1_800_000_000
	fixClock(t, now)
	c, db := newCollector(t)
	seedAudit(t, db, now)

	run, err := c.Run(context.Background(), Options{DryRun: true, Trigger: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Report.PrunedAuditEntries != 0 {
		t.Errorf("PrunedAuditEntries = %d on dry run, want 0", run.Report.PrunedAuditEntries)
	}
	if n := auditCount(t, db); n != 2 {
		t.Errorf("audit rows after dry run = %d, want 2", n)
	}
}

func TestZeroRetentionKeepsAudit(t *testing.T) {
	const now = 1_800_000_000
	fixClock(t, now)
	c, db := newCollector(t)
	s := DefaultSettings
	s.AuditRetentionDays = 0
	if err := c.SaveSettings(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	seedAudit(t, db, now)

	if _, err := c.Run(context.Background(), Options{Trigger: "manual"}); err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, db); n != 2 {
		t.Errorf("audit rows with retention 0 = %d, want 2", n)
	}
}

func TestDefaultAuditRetention(t *testing.T) {
	c, _ := newCollector(t)
	if got := c.Settings(context.Background()).AuditRetentionDays; got != 90 {
		t.Errorf("default AuditRetentionDays = %d, want 90", got)
	}
	s := DefaultSettings
	s.AuditRetentionDays = -1
	if err := s.Validate(); err == nil {
		t.Error("negative AuditRetentionDays validated")
	}
}

func TestOnFinishCalledForRealRunOnly(t *testing.T) {
	const now = 1_800_000_000
	fixClock(t, now)
	c, _ := newCollector(t)
	var calls []*Run
	c.OnFinish = func(_ context.Context, run *Run) { calls = append(calls, run) }
	ctx := context.Background()

	if _, err := c.Run(ctx, Options{DryRun: true, Trigger: "manual"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("OnFinish called %d times for dry run, want 0", len(calls))
	}

	run, err := c.Run(ctx, Options{Trigger: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("OnFinish called %d times for real run, want 1", len(calls))
	}
	if calls[0] != run {
		t.Error("OnFinish received a different run")
	}
}
