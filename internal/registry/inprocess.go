package registry

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/events"
)

// ImportHeader marks requests of an import. For admins, their events go to the
// audit log but trigger no webhooks; for anyone else the header is ignored, so
// a project member cannot silence webhooks a project admin configured.
const ImportHeader = "X-Doreg-Import"

type inProcessKey struct{}

// Transport serves requests in-process as principal p, so a server-side import
// pushes through the same handler (permissions, quotas, digest locks, events)
// as any client, without holding credentials. Responses are buffered; it is
// meant for pushes and existence checks, not for downloading blobs.
func (h *Handler) Transport(p auth.Principal) http.RoundTripper {
	return inProcess{h: h, p: p}
}

type inProcess struct {
	h *Handler
	p auth.Principal
}

func (t inProcess) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		defer req.Body.Close()
	}
	ctx := context.WithValue(req.Context(), inProcessKey{}, t.p)
	ctx = events.WithoutWebhooks(ctx)
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	r := req.Clone(ctx)
	if r.Body == nil {
		r.Body = http.NoBody
	}
	r.RemoteAddr = "in-process"
	r.Host = req.URL.Host
	t.h.ServeHTTP(rec, r)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &http.Response{
		Status:        http.StatusText(rec.status),
		StatusCode:    rec.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        rec.header,
		Body:          io.NopCloser(&rec.body),
		ContentLength: int64(rec.body.Len()),
		Request:       req,
	}, nil
}

type recorder struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = status, true
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(b)
}
