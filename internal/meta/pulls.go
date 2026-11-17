package meta

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// pullFlushInterval bounds how stale pull counters may be. Counts are buffered
// in memory so a pull stays a read; a crash loses at most this window.
const pullFlushInterval = 10 * time.Second

type pullCounter struct {
	mu      sync.Mutex
	pending map[int64]int64 // manifest id -> pulls not yet written
	stop    chan struct{}
	done    chan struct{}
}

func (db *DB) startPullFlusher() {
	db.pulls = &pullCounter{pending: map[int64]int64{}, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(db.pulls.done)
		t := time.NewTicker(pullFlushInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_ = db.FlushPulls(context.Background())
			case <-db.pulls.stop:
				_ = db.FlushPulls(context.Background())
				return
			}
		}
	}()
}

func (db *DB) stopPullFlusher() {
	close(db.pulls.stop)
	<-db.pulls.done
}

// countPull buffers one pull of manifest id.
func (db *DB) countPull(id int64) {
	db.pulls.mu.Lock()
	db.pulls.pending[id]++
	db.pulls.mu.Unlock()
}

// FlushPulls writes buffered pull counts. A repository's counter only grows
// for manifests that are neither an index child nor a referrer, so one
// multi-platform pull counts once. Manifests deleted in the meantime are
// skipped.
func (db *DB) FlushPulls(ctx context.Context) error {
	db.pulls.mu.Lock()
	pending := db.pulls.pending
	db.pulls.pending = map[int64]int64{}
	db.pulls.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		for id, n := range pending {
			if _, err := tx.ExecContext(ctx, `UPDATE repositories SET pull_count = pull_count + ?
				WHERE id = (SELECT m.repo_id FROM manifests m WHERE m.id = ? AND m.subject = ''
					AND NOT EXISTS (SELECT 1 FROM manifest_refs mr JOIN manifests p ON p.id = mr.manifest_id
						WHERE mr.kind = 'manifest' AND mr.digest = m.digest AND p.repo_id = m.repo_id))`,
				n, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE manifests SET pull_count = pull_count + ? WHERE id = ?`, n, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// Put the counts back so a transient failure does not lose them.
		db.pulls.mu.Lock()
		for id, n := range pending {
			db.pulls.pending[id] += n
		}
		db.pulls.mu.Unlock()
	}
	return err
}
