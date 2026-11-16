package gc

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/p-arndt/doreg/internal/meta"
)

// Policy is a tag retention rule.
type Policy struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	RepoPattern   string `json:"repoPattern"`   // glob; * also matches "/"
	TagPattern    string `json:"tagPattern"`    // regex; empty = every tag
	KeepPattern   string `json:"keepPattern"`   // regex; matching tags are never deleted
	KeepLast      int    `json:"keepLast"`      // keep the newest N candidate tags; 0 = no count limit
	OlderThanDays int    `json:"olderThanDays"` // only delete tags older than this; 0 = any age
	Enabled       bool   `json:"enabled"`
	CreatedAt     int64  `json:"createdAt"`
}

// Validate checks that the policy can be compiled and actually limits something.
func (p Policy) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(p.RepoPattern) == "" {
		return fmt.Errorf("repository pattern is required")
	}
	if p.KeepLast < 0 || p.OlderThanDays < 0 {
		return fmt.Errorf("keepLast and olderThanDays must not be negative")
	}
	if p.KeepLast == 0 && p.OlderThanDays == 0 {
		return fmt.Errorf("set keepLast or olderThanDays, otherwise the policy would delete every tag")
	}
	if _, err := regexp.Compile(p.TagPattern); err != nil {
		return fmt.Errorf("tag pattern: %w", err)
	}
	if _, err := regexp.Compile(p.KeepPattern); err != nil {
		return fmt.Errorf("keep pattern: %w", err)
	}
	return nil
}

// globToRegexp converts a repository glob. "*" matches across "/" because users
// expect "team/*" to cover "team/app/api" too.
func globToRegexp(g string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range g {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// TagDeletion is a tag that retention removes.
type TagDeletion struct {
	Repo      string `json:"repo"`
	Tag       string `json:"tag"`
	Digest    string `json:"digest"`
	UpdatedAt int64  `json:"updatedAt"`
	Policy    string `json:"policy"`
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type tagRow struct {
	repo, tag, digest string
	updated           int64
}

// Evaluate returns the tags the enabled policies would delete. A tag matched by
// several policies is deleted if any policy says so.
func Evaluate(ctx context.Context, q querier, policies []Policy, now int64) ([]TagDeletion, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT r.name, t.name, m.digest, t.updated_at
		FROM tags t JOIN repositories r ON r.id = t.repo_id JOIN manifests m ON m.id = t.manifest_id
		ORDER BY r.name, t.updated_at DESC, t.name`)
	if err != nil {
		return nil, err
	}
	byRepo := map[string][]tagRow{}
	var repos []string
	for rows.Next() {
		var t tagRow
		if err := rows.Scan(&t.repo, &t.tag, &t.digest, &t.updated); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := byRepo[t.repo]; !ok {
			repos = append(repos, t.repo)
		}
		byRepo[t.repo] = append(byRepo[t.repo], t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []TagDeletion
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("policy %q: %w", p.Name, err)
		}
		repoRe, err := globToRegexp(p.RepoPattern)
		if err != nil {
			return nil, err
		}
		tagRe := regexp.MustCompile(p.TagPattern)
		var keepRe *regexp.Regexp
		if p.KeepPattern != "" {
			keepRe = regexp.MustCompile(p.KeepPattern)
		}
		cutoff := now - int64(p.OlderThanDays)*86400
		for _, repo := range repos {
			if !repoRe.MatchString(repo) {
				continue
			}
			candidates := 0
			for _, t := range byRepo[repo] { // newest first
				if !tagRe.MatchString(t.tag) || (keepRe != nil && keepRe.MatchString(t.tag)) {
					continue
				}
				candidates++
				if p.KeepLast > 0 && candidates <= p.KeepLast {
					continue
				}
				if p.OlderThanDays > 0 && t.updated >= cutoff {
					continue
				}
				key := repo + ":" + t.tag
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, TagDeletion{Repo: repo, Tag: t.tag, Digest: t.digest, UpdatedAt: t.updated, Policy: p.Name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// Policies lists all retention policies.
func Policies(ctx context.Context, db *meta.DB) ([]Policy, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT id, name, repo_pattern, tag_pattern, keep_pattern, keep_last, older_than_days, enabled, created_at
		FROM retention_policies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		var p Policy
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPattern, &p.TagPattern, &p.KeepPattern,
			&p.KeepLast, &p.OlderThanDays, &p.Enabled, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SavePolicy inserts (ID 0) or updates a policy and returns its id.
func SavePolicy(ctx context.Context, db *meta.DB, p Policy) (int64, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if p.ID == 0 {
		var id int64
		err := db.W.QueryRowContext(ctx, `
			INSERT INTO retention_policies(name, repo_pattern, tag_pattern, keep_pattern, keep_last, older_than_days, enabled, created_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			p.Name, p.RepoPattern, p.TagPattern, p.KeepPattern, p.KeepLast, p.OlderThanDays, p.Enabled, meta.Now()).Scan(&id)
		return id, err
	}
	res, err := db.W.ExecContext(ctx, `
		UPDATE retention_policies SET name = ?, repo_pattern = ?, tag_pattern = ?, keep_pattern = ?,
			keep_last = ?, older_than_days = ?, enabled = ? WHERE id = ?`,
		p.Name, p.RepoPattern, p.TagPattern, p.KeepPattern, p.KeepLast, p.OlderThanDays, p.Enabled, p.ID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, meta.ErrNotFound
	}
	return p.ID, nil
}

// DeletePolicy removes a policy.
func DeletePolicy(ctx context.Context, db *meta.DB, id int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM retention_policies WHERE id = ?`, id)
	return err
}
