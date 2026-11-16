package api

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/meta"
)

const (
	// cliCodeAlphabet leaves out vowels and easily confused characters so the
	// code can be read aloud and typed without mistakes.
	cliCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"
	cliDeviceTTL    = 10 * time.Minute
	cliPollInterval = 2
	cliTokenTTL     = 90 * 24 * time.Hour
	maxCLIClient    = 80
	defaultCLIName  = "CLI"
)

// cliStart opens a device login. The CLI keeps the device code secret and
// shows the user code, which the user approves in the browser.
func (a *API) cliStart(w http.ResponseWriter, r *http.Request) {
	var body struct{ Client string }
	if !decode(w, r, &body) {
		return
	}
	client := truncateRunes(strings.TrimSpace(body.Client), maxCLIClient)
	userCode, err := newCLIUserCode()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	deviceCode := auth.NewSecret()
	now := meta.Now()
	err = a.DB.CreateDevice(r.Context(), auth.HashToken(deviceCode), meta.Device{
		UserCode:  userCode,
		Client:    client,
		CreatedAt: now,
		ExpiresAt: now + int64(cliDeviceTTL/time.Second),
	})
	if errors.Is(err, meta.ErrTooMany) {
		writeErr(w, http.StatusTooManyRequests, "too many pending logins, try again later")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deviceCode":      deviceCode,
		"userCode":        userCode,
		"verificationUrl": "/cli/approve?code=" + userCode,
		"interval":        cliPollInterval,
		"expiresIn":       int(cliDeviceTTL / time.Second),
	})
}

// cliPoll hands out the token of an approved device exactly once. Every
// other outcome is an OAuth-style error the CLI keeps polling on or stops at.
func (a *API) cliPoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"deviceCode"`
	}
	if !decode(w, r, &body) {
		return
	}
	d, err := a.DB.TakeDevice(r.Context(), auth.HashToken(body.DeviceCode))
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, http.StatusBadRequest, "expired_token")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	switch d.Status {
	case meta.DevicePending:
		writeErr(w, http.StatusBadRequest, "authorization_pending")
		return
	case meta.DeviceDenied:
		writeErr(w, http.StatusBadRequest, "access_denied")
		return
	case meta.DeviceApproved:
	default:
		writeErr(w, http.StatusBadRequest, "expired_token")
		return
	}
	u, err := a.DB.UserByID(r.Context(), d.UserID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	t, err := a.DB.CLITokenByHash(r.Context(), auth.HashToken(d.Token))
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, http.StatusBadRequest, "expired_token")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":  u.Username,
		"token":     d.Token,
		"expiresAt": t.ExpiresAt,
	})
}

// cliDeviceStatus shows the signed-in user what they are about to approve.
func (a *API) cliDeviceStatus(w http.ResponseWriter, r *http.Request) {
	d, err := a.DB.DeviceByUserCode(r.Context(), normalizeCLIUserCode(r.PathValue("code")))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// cliApprove issues a token for the device and hands it to the CLI on its
// next poll.
func (a *API) cliApprove(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := normalizeCLIUserCode(r.PathValue("code"))
	d, err := a.DB.DeviceByUserCode(r.Context(), code)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !cliPending(w, d) {
		return
	}
	name := d.Client
	if name == "" {
		name = defaultCLIName
	}
	token := auth.NewToken()
	tokenHash := auth.HashToken(token)
	if err := a.DB.CreateCLIToken(r.Context(), u.ID, name, tokenHash, meta.Now()+int64(cliTokenTTL/time.Second)); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.DB.DecideDevice(r.Context(), code, meta.DeviceApproved, u.ID, token); err != nil {
		a.dropCLIToken(r, u.ID, tokenHash)
		if errors.Is(err, meta.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "code is no longer pending")
			return
		}
		a.fail(w, r, err)
		return
	}
	a.record(r, "cli_login_approve", "", "", d.Client)
	w.WriteHeader(http.StatusNoContent)
}

// cliDeny rejects a pending device; the CLI learns it on its next poll.
func (a *API) cliDeny(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := normalizeCLIUserCode(r.PathValue("code"))
	d, err := a.DB.DeviceByUserCode(r.Context(), code)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !cliPending(w, d) {
		return
	}
	err = a.DB.DecideDevice(r.Context(), code, meta.DeviceDenied, u.ID, "")
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, http.StatusBadRequest, "code is no longer pending")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "cli_login_deny", "", "", d.Client)
	w.WriteHeader(http.StatusNoContent)
}

// cliTokens lists the signed-in user's CLI tokens. Hashes are never exposed.
func (a *API) cliTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := a.DB.CLITokens(r.Context(), currentUser(r).ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// cliRevokeToken deletes one of the user's CLI tokens. The credential cache
// is dropped so the token stops working at once.
func (a *API) cliRevokeToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	u := currentUser(r)
	if err := a.DB.DeleteCLIToken(r.Context(), u.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Auth.Invalidate()
	a.record(r, "cli_token_revoke", "", strconv.FormatInt(id, 10), "")
	w.WriteHeader(http.StatusNoContent)
}

// cliPending writes the error and reports false unless the device still awaits
// a decision.
func cliPending(w http.ResponseWriter, d meta.Device) bool {
	switch d.Status {
	case meta.DevicePending:
		return true
	case meta.DeviceExpired:
		writeErr(w, http.StatusBadRequest, "code has expired")
	default:
		writeErr(w, http.StatusBadRequest, "code was already used")
	}
	return false
}

// dropCLIToken removes a token that was created for a decision that did not
// go through, so no unissued credential stays listed.
func (a *API) dropCLIToken(r *http.Request, userID int64, tokenHash string) {
	t, err := a.DB.CLITokenByHash(r.Context(), tokenHash)
	if err != nil {
		return
	}
	_ = a.DB.DeleteCLIToken(r.Context(), userID, t.ID)
}

// normalizeCLIUserCode accepts the code in any case and with or without its
// dash, as users type it.
func normalizeCLIUserCode(code string) string {
	code = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	if len(code) == 8 {
		return code[:4] + "-" + code[4:]
	}
	return code
}

func newCLIUserCode() (string, error) {
	b := make([]byte, 8)
	size := big.NewInt(int64(len(cliCodeAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", err
		}
		b[i] = cliCodeAlphabet[n.Int64()]
	}
	return string(b[:4]) + "-" + string(b[4:]), nil
}
