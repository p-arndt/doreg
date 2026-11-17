package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/p-arndt/doreg/internal/auth"
)

// tokenPath is the realm of the Bearer challenge. Docker only lets anonymous
// clients pull after a Bearer challenge: on a Basic one it refuses to send a
// request without stored credentials, even for public repositories.
const tokenPath = "/v2/token"

// service names this registry in challenges and token requests.
const service = "doreg"

// token issues a Bearer token for Basic credentials (GET, as Docker sends
// them) or an OAuth password grant (POST, as containerd tries first), and
// the anonymous token without credentials. Tokens carry no scope: every
// request is still authorized per repository and action.
func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	name, pw, hasCreds := r.BasicAuth()
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "UNSUPPORTED", "invalid form")
			return
		}
		if r.PostForm.Get("grant_type") != "password" {
			writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
			return
		}
		name, pw, hasCreds = r.PostForm.Get("username"), r.PostForm.Get("password"), true
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
		return
	}
	p := auth.Anonymous
	if hasCreds {
		var status int
		if p, status = h.basic(w, r, name, pw); status != 0 {
			if status == http.StatusUnauthorized {
				writeError(w, status, "UNAUTHORIZED", "invalid username or password")
			}
			return
		}
	}
	tok := h.Auth.IssueToken(p)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{
		"token":        tok,
		"access_token": tok,
		"expires_in":   int(auth.TokenTTL / time.Second),
		"issued_at":    time.Now().UTC().Format(time.RFC3339),
	})
}

// basic checks Basic credentials under the failed-login limiter. A non-zero
// status means failure; for 429 the response is already written.
func (h *Handler) basic(w http.ResponseWriter, r *http.Request, name, pw string) (auth.Principal, int) {
	ip := clientIP(r)
	if ok, wait := h.Auth.Allowed(ip, name); !ok {
		w.Header().Set("Retry-After", auth.RetryAfter(wait))
		writeError(w, http.StatusTooManyRequests, "TOOMANYREQUESTS", "too many failed authentication attempts")
		return auth.Anonymous, http.StatusTooManyRequests
	}
	p, ok := h.Auth.Basic(r.Context(), name, pw)
	if !ok {
		h.Auth.Failed(ip, name)
		return auth.Anonymous, http.StatusUnauthorized
	}
	h.Auth.Succeeded(ip, name)
	return p, 0
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(tok), true
}

// challenge answers 401 with a Bearer challenge. errCode is the RFC 6750
// error ("" when the client sent no credentials).
func challenge(w http.ResponseWriter, r *http.Request, rt route, errCode string) bool {
	v := fmt.Sprintf(`Bearer realm=%q,service=%q`, baseURL(r)+tokenPath, service)
	if rt.name != "" {
		actions := "pull"
		if actionFor(r.Method) != auth.Pull {
			actions = "pull,push"
		}
		v += fmt.Sprintf(`,scope="repository:%s:%s"`, rt.name, actions)
	} else if rt.kind == "catalog" {
		v += `,scope="registry:catalog:*"`
	}
	if errCode != "" {
		v += fmt.Sprintf(`,error=%q`, errCode)
	}
	w.Header().Set("WWW-Authenticate", v)
	writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
	return false
}

// baseURL is the external URL clients reached us at; doreg runs behind a
// TLS-terminating proxy, which sets X-Forwarded-Proto and keeps Host.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
		scheme = p
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host, _, _ = strings.Cut(fh, ",")
		host = strings.TrimSpace(host)
	}
	return scheme + "://" + host
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code})
}
