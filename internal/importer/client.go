// Package importer copies repositories from another OCI registry (for example
// distribution/registry:2) into doreg.
package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/p-arndt/doreg/internal/oci"
)

// ErrUnauthorized means the registry wants credentials that were missing or wrong.
var ErrUnauthorized = errors.New("the registry rejected the credentials")

// maxManifestSize matches the limit of the registry handler.
const maxManifestSize = 4 << 20

var manifestAccept = strings.Join([]string{
	oci.MediaTypeOCIIndex, oci.MediaTypeOCIManifest,
	oci.MediaTypeDockerList, oci.MediaTypeDockerManifest,
}, ", ")

// Credentials log in to a registry. Both empty means anonymous.
type Credentials struct {
	Username string
	Password string
}

// Client talks the OCI distribution API to one registry. It answers both
// Basic and Bearer (token server) challenges, so it works with registry:2
// behind htpasswd or token auth as well as with hosted registries.
type Client struct {
	base   *url.URL
	creds  Credentials
	http   *http.Client
	header http.Header

	mu     sync.Mutex
	basic  bool              // the registry asked for Basic auth
	tokens map[string]string // bearer tokens by scope
}

// NewClient returns a client for the registry at rawURL ("host:port" means
// https). transport nil uses http.DefaultTransport. header is sent with every
// registry request.
func NewClient(rawURL string, creds Credentials, transport http.RoundTripper, header http.Header) (*Client, error) {
	u, err := ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Client{base: u, creds: creds, http: &http.Client{Transport: transport}, header: header,
		tokens: map[string]string{}}, nil
}

// ParseURL normalises a registry address: a missing scheme means https, and a
// trailing /v2/ is dropped.
func ParseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("registry URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid registry URL: scheme must be http or https")
	}
	if u.Host == "" {
		return nil, errors.New("invalid registry URL: missing host")
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/v2"), "/")
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u, nil
}

// Host returns the registry address without credentials, for logs and the audit log.
func (c *Client) Host() string { return c.base.String() }

func pullScope(repo string) string { return "repository:" + repo + ":pull" }
func pushScope(repo string) string { return "repository:" + repo + ":pull,push" }

const catalogScope = "registry:catalog:*"

// send performs a request built by newReq, answering one auth challenge. newReq
// is called again for the retry, so request bodies need no buffering.
func (c *Client) send(ctx context.Context, scope string, newReq func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := newReq()
		if err != nil {
			return nil, err
		}
		req = req.WithContext(ctx)
		for k, v := range c.header {
			req.Header[k] = v
		}
		c.authorize(req, scope)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt > 0 {
			if resp.StatusCode == http.StatusUnauthorized {
				resp.Body.Close()
				return nil, ErrUnauthorized
			}
			return resp, nil
		}
		challenge := resp.Header.Get("WWW-Authenticate")
		drain(resp)
		if err := c.answer(ctx, challenge, scope); err != nil {
			return nil, err
		}
	}
}

func (c *Client) authorize(req *http.Request, scope string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tok, ok := c.tokens[scope]; ok {
		req.Header.Set("Authorization", "Bearer "+tok)
	} else if c.basic {
		req.SetBasicAuth(c.creds.Username, c.creds.Password)
	}
}

// answer prepares credentials for a challenge.
func (c *Client) answer(ctx context.Context, challenge, scope string) error {
	scheme, params := parseChallenge(challenge)
	switch scheme {
	case "basic":
		if c.creds.Username == "" {
			return fmt.Errorf("%w: the registry requires a login", ErrUnauthorized)
		}
		c.mu.Lock()
		c.basic = true
		c.mu.Unlock()
		return nil
	case "bearer":
		tok, err := c.fetchToken(ctx, params["realm"], params["service"], scope)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.tokens[scope] = tok
		c.mu.Unlock()
		return nil
	}
	return fmt.Errorf("%w: unsupported authentication %q", ErrUnauthorized, challenge)
}

func (c *Client) fetchToken(ctx context.Context, realm, service, scope string) (string, error) {
	u, err := url.Parse(realm)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid token realm %q", realm)
	}
	q := u.Query()
	if service != "" {
		q.Set("service", service)
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if c.creds.Username != "" {
		req.SetBasicAuth(c.creds.Username, c.creds.Password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("token response: %w", err)
	}
	if body.Token == "" {
		body.Token = body.AccessToken
	}
	if body.Token == "" {
		return "", errors.New("token response contains no token")
	}
	return body.Token, nil
}

// parseChallenge splits a WWW-Authenticate value into its lower-cased scheme
// and parameters. Quoted values may contain commas (scope="a:b:pull,push").
func parseChallenge(s string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(s), " ")
	params := map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; {
		key, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		key = strings.ToLower(strings.TrimSpace(key))
		var val string
		if strings.HasPrefix(after, "\"") {
			end := strings.IndexByte(after[1:], '"')
			if end < 0 {
				val, after = after[1:], ""
			} else {
				val, after = after[1:end+1], after[end+2:]
			}
		} else {
			val, after, _ = strings.Cut(after, ",")
			after = "," + after
		}
		params[key] = val
		rest = strings.TrimLeft(strings.TrimSpace(after), ", ")
	}
	return strings.ToLower(scheme), params
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
}

// statusError turns an unexpected response into an error carrying the
// registry's message.
func statusError(resp *http.Response) error {
	defer drain(resp)
	var body struct {
		Errors []struct{ Code, Message string } `json:"errors"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	msg := http.StatusText(resp.StatusCode)
	if len(body.Errors) > 0 {
		msg = body.Errors[0].Code + ": " + body.Errors[0].Message
	}
	return fmt.Errorf("%s %s: HTTP %d: %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, msg)
}

func (c *Client) url(path string, query url.Values) string {
	u := *c.base
	u.Path = c.base.Path + path
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *Client) get(ctx context.Context, method, scope, target string, header http.Header) (*http.Response, error) {
	return c.send(ctx, scope, func() (*http.Request, error) {
		req, err := http.NewRequest(method, target, nil)
		if err == nil && header != nil {
			req.Header = header.Clone()
		}
		return req, err
	})
}

// Ping checks that the registry answers and accepts the credentials.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.get(ctx, http.MethodGet, "", c.url("/v2/", nil), nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	drain(resp)
	return nil
}

// list follows the Link pagination of the catalog and tag list endpoints.
func (c *Client) list(ctx context.Context, scope, path, key string) ([]string, error) {
	var all []string
	target := c.url(path, url.Values{"n": {"1000"}})
	for target != "" {
		resp, err := c.get(ctx, http.MethodGet, scope, target, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, statusError(resp)
		}
		var page map[string]json.RawMessage
		err = json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&page)
		next := nextLink(resp)
		drain(resp)
		if err != nil {
			return nil, fmt.Errorf("GET %s: %w", path, err)
		}
		var items []string
		if raw := page[key]; raw != nil {
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, fmt.Errorf("GET %s: %w", path, err)
			}
		}
		all = append(all, items...)
		target = next
	}
	return all, nil
}

// nextLink resolves the rel="next" Link header against the request URL.
func nextLink(resp *http.Response) string {
	for _, link := range resp.Header.Values("Link") {
		ref, params, _ := strings.Cut(link, ";")
		if !strings.Contains(params, `rel="next"`) {
			continue
		}
		ref = strings.Trim(strings.TrimSpace(ref), "<>")
		u, err := resp.Request.URL.Parse(ref)
		if err == nil {
			return u.String()
		}
	}
	return ""
}

// Catalog lists all repositories the credentials can see.
func (c *Client) Catalog(ctx context.Context) ([]string, error) {
	return c.list(ctx, catalogScope, "/v2/_catalog", "repositories")
}

// Tags lists the tags of a repository.
func (c *Client) Tags(ctx context.Context, repo string) ([]string, error) {
	return c.list(ctx, pullScope(repo), "/v2/"+repo+"/tags/list", "tags")
}

// Manifest fetches a manifest by tag or digest, byte for byte.
func (c *Client) Manifest(ctx context.Context, repo, ref string) (body []byte, mediaType string, err error) {
	resp, err := c.get(ctx, http.MethodGet, pullScope(repo), c.url("/v2/"+repo+"/manifests/"+ref, nil),
		http.Header{"Accept": {manifestAccept}})
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", statusError(resp)
	}
	defer drain(resp)
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxManifestSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxManifestSize {
		return nil, "", fmt.Errorf("manifest %s:%s is larger than %d bytes", repo, ref, maxManifestSize)
	}
	mediaType, _, _ = strings.Cut(resp.Header.Get("Content-Type"), ";")
	return body, strings.TrimSpace(mediaType), nil
}

// ManifestDigest returns the digest a tag or digest reference resolves to, or
// "" if the registry does not know it.
func (c *Client) ManifestDigest(ctx context.Context, repo, ref string) (string, error) {
	resp, err := c.get(ctx, http.MethodHead, pushScope(repo), c.url("/v2/"+repo+"/manifests/"+ref, nil),
		http.Header{"Accept": {manifestAccept}})
	if err != nil {
		return "", err
	}
	defer drain(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Header.Get("Docker-Content-Digest"), nil
	case http.StatusNotFound:
		return "", nil
	}
	return "", statusError(resp)
}

// HasBlob reports whether the repository holds the blob.
func (c *Client) HasBlob(ctx context.Context, repo, dgst string) (bool, error) {
	resp, err := c.get(ctx, http.MethodHead, pushScope(repo), c.url("/v2/"+repo+"/blobs/"+dgst, nil), nil)
	if err != nil {
		return false, err
	}
	defer drain(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, statusError(resp)
}

// Blob opens a blob for reading. Redirects to object storage are followed.
func (c *Client) Blob(ctx context.Context, repo, dgst string) (io.ReadCloser, int64, error) {
	resp, err := c.get(ctx, http.MethodGet, pullScope(repo), c.url("/v2/"+repo+"/blobs/"+dgst, nil), nil)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, statusError(resp)
	}
	return resp.Body, resp.ContentLength, nil
}

// PushBlob stores a blob. It first offers a cross-repository mount, which
// succeeds without any transfer when the registry already holds the blob in
// another repository; otherwise open supplies the content.
func (c *Client) PushBlob(ctx context.Context, repo, dgst string, open func() (io.ReadCloser, int64, error)) (mounted bool, err error) {
	resp, err := c.send(ctx, pushScope(repo), func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, c.url("/v2/"+repo+"/blobs/uploads/", url.Values{"mount": {dgst}}), nil)
	})
	if err != nil {
		return false, err
	}
	switch resp.StatusCode {
	case http.StatusCreated:
		drain(resp)
		return true, nil
	case http.StatusAccepted:
	default:
		return false, statusError(resp)
	}
	drain(resp)
	loc, err := resp.Request.URL.Parse(resp.Header.Get("Location"))
	if err != nil || resp.Header.Get("Location") == "" {
		return false, fmt.Errorf("upload of %s: missing or invalid Location", dgst)
	}
	q := loc.Query()
	q.Set("digest", dgst)
	loc.RawQuery = q.Encode()
	resp, err = c.send(ctx, pushScope(repo), func() (*http.Request, error) {
		body, size, err := open()
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPut, loc.String(), body)
		if err != nil {
			body.Close()
			return nil, err
		}
		req.ContentLength = size
		req.Header.Set("Content-Type", "application/octet-stream")
		return req, nil
	})
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusCreated {
		return false, statusError(resp)
	}
	drain(resp)
	return false, nil
}

// PutManifest stores a manifest under a tag or its digest.
func (c *Client) PutManifest(ctx context.Context, repo, ref string, body []byte, mediaType string) error {
	resp, err := c.send(ctx, pushScope(repo), func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPut, c.url("/v2/"+repo+"/manifests/"+ref, nil), bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", mediaType)
		}
		return req, err
	})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated {
		return statusError(resp)
	}
	drain(resp)
	return nil
}
