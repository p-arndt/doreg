package auth

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/p-arndt/doreg/internal/meta"
)

// TokenTTL is the lifetime of registry Bearer tokens. Clients fetch a new one
// when it runs out, so it can stay as short as the credential cache.
const TokenTTL = cacheTTL

// AnonymousToken is the Bearer token of unauthenticated clients. It grants
// nothing an unauthenticated request does not already get, so it needs no
// state and can be handed out without limit.
const AnonymousToken = "anonymous"

type issued struct {
	p       Principal
	expires time.Time
	gen     uint64
}

// IssueToken returns a Bearer token for p. Tokens live in memory: a restart
// makes clients fetch new ones, which they do on the next 401 anyway.
func (a *Authenticator) IssueToken(p Principal) string {
	if p.Kind() == "anonymous" {
		return AnonymousToken
	}
	tok := NewSecret()
	now := time.Now()
	a.mu.Lock()
	for k, t := range a.tokens {
		if now.After(t.expires) {
			delete(a.tokens, k)
		}
	}
	a.tokens[sha256.Sum256([]byte(tok))] = issued{p: p, expires: now.Add(TokenTTL), gen: a.gen}
	a.mu.Unlock()
	return tok
}

// Bearer resolves a token from IssueToken. After Invalidate, the principal is
// reloaded once, so deleted accounts and role changes apply to tokens already
// handed out instead of after they expire.
func (a *Authenticator) Bearer(ctx context.Context, tok string) (Principal, bool) {
	if tok == AnonymousToken {
		return Anonymous, true
	}
	key := sha256.Sum256([]byte(tok))
	now := time.Now()
	a.mu.Lock()
	t, ok := a.tokens[key]
	gen := a.gen
	a.mu.Unlock()
	if !ok || now.After(t.expires) {
		return Anonymous, false
	}
	if t.gen != gen {
		p, ok := a.reload(ctx, t.p)
		a.mu.Lock()
		if !ok {
			delete(a.tokens, key)
		} else if _, still := a.tokens[key]; still {
			a.tokens[key] = issued{p: p, expires: t.expires, gen: gen}
		}
		a.mu.Unlock()
		if !ok {
			return Anonymous, false
		}
		t.p = p
	}
	if t.p.Robot != nil && !t.p.Robot.Active(meta.Now()) {
		return Anonymous, false
	}
	return t.p, true
}

func (a *Authenticator) reload(ctx context.Context, p Principal) (Principal, bool) {
	switch {
	case p.User != nil:
		u, err := a.db.UserByID(ctx, p.User.ID)
		if err != nil {
			return Anonymous, false
		}
		return Principal{User: &u}, true
	case p.Robot != nil:
		r, err := a.db.RobotByID(ctx, p.Robot.ID)
		if err != nil {
			return Anonymous, false
		}
		return Principal{Robot: &r}, true
	}
	return Anonymous, false
}
