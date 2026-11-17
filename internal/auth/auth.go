// Package auth resolves credentials to principals and decides what they may do.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/p-arndt/doreg/internal/meta"
)

// cacheTTL bounds how long verified credentials stay valid without bcrypt.
// Clients send Basic credentials on every request and a push is hundreds of
// requests; bcrypt on each would cost seconds.
const cacheTTL = 5 * time.Minute

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("doreg-dummy"), bcrypt.DefaultCost)

// Principal is whoever makes a request.
type Principal struct {
	User  *meta.User
	Robot *meta.Robot
}

// Anonymous is the principal of unauthenticated requests.
var Anonymous = Principal{}

// Kind is "user", "robot" or "anonymous".
func (p Principal) Kind() string {
	switch {
	case p.User != nil:
		return "user"
	case p.Robot != nil:
		return "robot"
	}
	return "anonymous"
}

// Name identifies the principal in audit logs.
func (p Principal) Name() string {
	switch {
	case p.User != nil:
		return p.User.Username
	case p.Robot != nil:
		return p.Robot.Name
	}
	return "anonymous"
}

// IsAdmin reports system admin permission.
func (p Principal) IsAdmin() bool { return p.User != nil && p.User.IsAdmin() }

// Action is something done to a project's content.
type Action int

// Actions, in increasing privilege.
const (
	Pull Action = iota + 1
	Push
	Delete
	Manage // members, robots, webhooks, repository settings
)

// Authenticator checks credentials and permissions.
type Authenticator struct {
	db    *meta.DB
	mu    sync.Mutex
	cache map[[32]byte]cached

	tokens map[[32]byte]issued
	gen    uint64 // bumped by Invalidate; tokens of older generations are rechecked

	// Limiter throttles failed credential checks of the UI login and registry
	// Basic auth; both share it so a client cannot split guesses across them.
	Limiter *Limiter
}

type cached struct {
	p       Principal
	expires time.Time
}

// New returns an Authenticator.
func New(db *meta.DB) *Authenticator {
	return &Authenticator{db: db, cache: map[[32]byte]cached{}, tokens: map[[32]byte]issued{}, Limiter: NewLimiter()}
}

// Allowed reports whether a credential check from ip for username may run.
func (a *Authenticator) Allowed(ip, username string) (bool, time.Duration) {
	return a.Limiter.Allowed(ip, username)
}

// Failed records a failed credential check.
func (a *Authenticator) Failed(ip, username string) { a.Limiter.Failed(ip, username) }

// Succeeded clears the failure count of ip and username.
func (a *Authenticator) Succeeded(ip, username string) { a.Limiter.Succeeded(ip, username) }

// HashPassword returns a bcrypt hash.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// TokenPrefix marks tokens issued by `doreg login`, so Basic auth can look
// them up by hash instead of trying bcrypt.
const TokenPrefix = "doreg_"

// NewToken returns a random `doreg login` token.
func NewToken() string { return TokenPrefix + NewSecret() }

// HashToken is the lookup key stored for a token.
func HashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// NewSecret returns a random secret for robots and CLI logins.
func NewSecret() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func matches(hash, secret string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) == nil
}

// Check verifies a username and password (or CLI secret) of a user account.
// SSO accounts have no password and log in to the registry with their CLI secret.
func (a *Authenticator) Check(ctx context.Context, username, password string) (meta.User, bool) {
	p, ok := a.Basic(ctx, username, password)
	if !ok || p.User == nil {
		return meta.User{}, false
	}
	return *p.User, true
}

// CheckPassword verifies only the account password (UI login).
func (a *Authenticator) CheckPassword(ctx context.Context, username, password string) (meta.User, bool) {
	u, err := a.db.UserByName(ctx, username)
	if err != nil || u.PasswordHash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return meta.User{}, false
	}
	return u, matches(u.PasswordHash, password)
}

// Basic resolves registry credentials: a robot secret, a user password or a
// user CLI secret.
func (a *Authenticator) Basic(ctx context.Context, username, secret string) (Principal, bool) {
	key := sha256.Sum256([]byte(username + "\x00" + secret))
	now := time.Now()
	a.mu.Lock()
	if c, ok := a.cache[key]; ok && now.Before(c.expires) {
		a.mu.Unlock()
		if c.p.Robot != nil && !c.p.Robot.Active(meta.Now()) {
			return Anonymous, false
		}
		return c.p, true
	}
	a.mu.Unlock()

	var p Principal
	if meta.IsRobotName(username) {
		r, err := a.db.RobotByName(ctx, username)
		if err != nil {
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(secret))
			return Anonymous, false
		}
		if !r.Active(meta.Now()) || !matches(r.SecretHash, secret) {
			return Anonymous, false
		}
		a.db.TouchRobot(ctx, r)
		p.Robot = &r
	} else {
		u, err := a.db.UserByName(ctx, username)
		if err != nil {
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(secret))
			return Anonymous, false
		}
		if strings.HasPrefix(secret, TokenPrefix) {
			t, err := a.db.CLITokenByHash(ctx, HashToken(secret))
			if err != nil || t.UserID != u.ID {
				return Anonymous, false
			}
			a.db.TouchCLIToken(ctx, t)
		} else if !matches(u.PasswordHash, secret) && !matches(u.CLISecret, secret) {
			return Anonymous, false
		}
		p.User = &u
	}

	a.mu.Lock()
	for k, c := range a.cache {
		if now.After(c.expires) {
			delete(a.cache, k)
		}
	}
	a.cache[key] = cached{p: p, expires: now.Add(cacheTTL)}
	a.mu.Unlock()
	return p, true
}

// Invalidate drops all cached credentials, e.g. after a password, secret or
// role change, and makes issued tokens reload their principal.
func (a *Authenticator) Invalidate() {
	a.mu.Lock()
	a.cache = map[[32]byte]cached{}
	a.gen++
	a.mu.Unlock()
}

// Role returns the principal's effective project role ("" = none).
func (a *Authenticator) Role(ctx context.Context, p Principal, project string) (string, error) {
	switch {
	case p.IsAdmin():
		return meta.ProjectAdmin, nil
	case p.User != nil:
		return a.db.MemberRole(ctx, project, p.User.ID, p.User.Groups)
	case p.Robot != nil:
		if p.Robot.Project != "" && p.Robot.Project != project {
			return "", nil
		}
		if p.Robot.CanPush || p.Robot.CanDelete {
			return meta.ProjectWrite, nil
		}
		return meta.ProjectRead, nil
	}
	return "", nil
}

// Can decides whether p may perform action in project. A project that does
// not exist yet only admits system admins (who create it on first push).
func (a *Authenticator) Can(ctx context.Context, p Principal, project string, action Action) (bool, error) {
	if p.IsAdmin() {
		return true, nil
	}
	proj, err := a.db.ProjectByName(ctx, project)
	if errors.Is(err, meta.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if p.Robot != nil {
		r := p.Robot
		if !r.Active(meta.Now()) || (r.Project != "" && r.Project != project) {
			return false, nil
		}
		switch action {
		case Pull:
			return true, nil
		case Push:
			return r.CanPush, nil
		case Delete:
			return r.CanDelete, nil
		}
		return false, nil
	}
	if action == Pull && proj.Public {
		return true, nil
	}
	if p.User == nil {
		return false, nil
	}
	role, err := a.db.MemberRole(ctx, project, p.User.ID, p.User.Groups)
	if err != nil {
		return false, err
	}
	rank := meta.RoleRank(role)
	switch action {
	case Pull:
		return rank >= 1, nil
	case Push, Delete:
		return rank >= 2, nil
	case Manage:
		return rank >= 3, nil
	}
	return false, nil
}
