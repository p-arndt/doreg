// Package oidc implements single sign-on with an OpenID Connect provider
// (authorization code flow with PKCE).
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config comes from the OIDC_* environment variables shared by all of
// p-arndt's projects.
type Config struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	ProviderName  string   // button label, e.g. "Authentik"
	RedirectURI   string   // https://reg.example.com/api/auth/oidc/callback
	Scopes        []string // default: openid, profile, email
	UsernameClaim string   // default preferred_username
	GroupsClaim   string   // empty: merge "groups", "roles" and Keycloak's realm_access.roles
	AdminRole     string   // users with this group/role become system admins
	AdminEmails   []string // users with one of these verified emails become system admins
}

// ConfigFromEnv reads the configuration. It returns nil when OIDC is not configured.
func ConfigFromEnv() (*Config, error) {
	issuer := os.Getenv("OIDC_ISSUER")
	if issuer == "" {
		return nil, nil
	}
	c := &Config{
		Issuer:        issuer,
		ClientID:      os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret:  os.Getenv("OIDC_CLIENT_SECRET"),
		ProviderName:  envOr("OIDC_PROVIDER_NAME", "Single sign-on"),
		RedirectURI:   os.Getenv("OIDC_REDIRECT_URI"),
		Scopes:        splitList(envOr("OIDC_SCOPES", "openid,profile,email")),
		UsernameClaim: envOr("OIDC_USERNAME_CLAIM", "preferred_username"),
		GroupsClaim:   os.Getenv("OIDC_GROUPS_CLAIM"),
		AdminRole:     os.Getenv("OIDC_ADMIN_ROLE"),
	}
	for _, e := range splitList(os.Getenv("OIDC_ADMIN_EMAILS")) {
		c.AdminEmails = append(c.AdminEmails, strings.ToLower(e))
	}
	if c.ClientID == "" || c.ClientSecret == "" || c.RedirectURI == "" {
		return nil, errors.New("OIDC_ISSUER needs OIDC_CLIENT_ID, OIDC_CLIENT_SECRET and OIDC_REDIRECT_URI (<public url>/api/auth/oidc/callback)")
	}
	return c, nil
}

// splitList accepts comma- or space-separated values.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Identity is what doreg takes from a verified ID token.
type Identity struct {
	Subject       string // issuer|sub, stable across username changes
	Username      string
	Email         string
	EmailVerified bool
	Groups        []string
}

// Provider runs the login flow.
type Provider struct {
	cfg      Config
	verifier *gooidc.IDTokenVerifier
	oauth    oauth2.Config

	mu      sync.Mutex
	pending map[string]pendingLogin
}

type pendingLogin struct {
	verifier string
	nonce    string
	next     string
	expires  time.Time
}

// New discovers the provider's endpoints.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	p, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &Provider{
		cfg:      cfg,
		verifier: p.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     p.Endpoint(),
			RedirectURL:  cfg.RedirectURI,
			Scopes:       cfg.Scopes,
		},
		pending: map[string]pendingLogin{},
	}, nil
}

// Name is the label for the login button.
func (p *Provider) Name() string { return p.cfg.ProviderName }

// AdminRule decides whether id is a system admin. configured is false when
// neither OIDC_ADMIN_ROLE nor OIDC_ADMIN_EMAILS is set; then doreg keeps roles
// as they are (and makes the first SSO user admin while there is none).
func (p *Provider) AdminRule(id Identity) (configured, admin bool) {
	return p.cfg.adminRule(id)
}

func (c Config) adminRule(id Identity) (configured, admin bool) {
	if c.AdminRole == "" && len(c.AdminEmails) == 0 {
		return false, false
	}
	for _, g := range id.Groups {
		if c.AdminRole != "" && g == c.AdminRole {
			return true, true
		}
	}
	// An unverified address could be anyone's; it never grants admin.
	if id.EmailVerified && id.Email != "" {
		for _, e := range c.AdminEmails {
			if strings.EqualFold(e, id.Email) {
				return true, true
			}
		}
	}
	return true, false
}

func random() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start begins a login and returns the provider URL and the state value the
// caller binds to the browser (cookie).
func (p *Provider) Start(next string) (redirect, state string) {
	state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
	p.mu.Lock()
	now := time.Now()
	for k, v := range p.pending {
		if now.After(v.expires) {
			delete(p.pending, k)
		}
	}
	p.pending[state] = pendingLogin{verifier: verifier, nonce: nonce, next: next, expires: now.Add(10 * time.Minute)}
	p.mu.Unlock()
	return p.oauth.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), state
}

// Finish exchanges the code and verifies the ID token. cookieState is the
// state stored in the browser; it must match the state the provider returned.
func (p *Provider) Finish(ctx context.Context, state, cookieState, code string) (Identity, string, error) {
	if state == "" || state != cookieState {
		return Identity{}, "", errors.New("login state mismatch, start the sign-in again")
	}
	p.mu.Lock()
	pl, ok := p.pending[state]
	delete(p.pending, state)
	p.mu.Unlock()
	if !ok || time.Now().After(pl.expires) {
		return Identity{}, "", errors.New("login expired, start the sign-in again")
	}
	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(pl.verifier))
	if err != nil {
		return Identity{}, "", fmt.Errorf("token exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return Identity{}, "", errors.New("provider returned no id_token")
	}
	idt, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, "", fmt.Errorf("id token: %w", err)
	}
	if idt.Nonce != pl.nonce {
		return Identity{}, "", errors.New("id token nonce mismatch")
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return Identity{}, "", err
	}
	id := Identity{Subject: idt.Issuer + "|" + idt.Subject}
	id.Username, _ = claims[p.cfg.UsernameClaim].(string)
	id.Email, _ = claims["email"].(string)
	switch v := claims["email_verified"].(type) {
	case bool:
		id.EmailVerified = v
	case string: // some providers send "true"
		id.EmailVerified = v == "true"
	}
	if id.Username == "" {
		id.Username, _, _ = strings.Cut(id.Email, "@")
	}
	if id.Username == "" {
		id.Username = idt.Subject
	}
	if p.cfg.GroupsClaim != "" {
		id.Groups = stringList(claims[p.cfg.GroupsClaim])
	} else {
		// Authentik and Entra send "groups", Keycloak and minicloak "roles" and
		// realm_access.roles; take whatever the provider sends.
		seen := map[string]bool{}
		add := func(v any) {
			for _, g := range stringList(v) {
				if !seen[g] {
					seen[g] = true
					id.Groups = append(id.Groups, g)
				}
			}
		}
		add(claims["groups"])
		add(claims["roles"])
		if ra, ok := claims["realm_access"].(map[string]any); ok {
			add(ra["roles"])
		}
	}
	if id.Groups == nil {
		id.Groups = []string{}
	}
	return id, pl.next, nil
}

func stringList(v any) []string {
	switch g := v.(type) {
	case []any:
		out := make([]string, 0, len(g))
		for _, x := range g {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{g}
	}
	return nil
}
