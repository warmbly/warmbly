package repository

import (
	"context"
	"io"
	"net/http"
	"time"
)

// controlPlaneTransport is shared by the worker's internal API clients: http.DefaultTransport keeps only 2 idle connections per host.
var controlPlaneTransport = newControlPlaneTransport()

func newControlPlaneTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return drainingTransport{next: http.DefaultTransport}
	}
	t := base.Clone()
	t.MaxIdleConns = 512
	t.MaxIdleConnsPerHost = 512
	return drainingTransport{next: t}
}

func newControlPlaneClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: controlPlaneTransport, Timeout: timeout}
}

// drainingTransport makes Close read what is left of a body (up to 64 KiB) so the connection is reused even when the caller never read it.
type drainingTransport struct {
	next http.RoundTripper
}

func (t drainingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	resp, err := t.next.RoundTrip(req.Clone(ctx))
	if resp != nil && resp.Body != nil {
		resp.Body = drainingBody{ReadCloser: resp.Body, cancel: cancel}
	} else {
		cancel()
	}
	return resp, err
}

type drainingBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b drainingBody) Close() error {
	// Cancel a stalled read without spending the request's remaining timeout.
	timer := time.AfterFunc(100*time.Millisecond, b.cancel)
	defer timer.Stop()
	defer b.cancel()
	_, _ = io.Copy(io.Discard, io.LimitReader(b.ReadCloser, 64<<10))
	return b.ReadCloser.Close()
}
