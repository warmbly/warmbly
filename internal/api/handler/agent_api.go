package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/app/aitools"
)

// agentAPIResponseCap bounds what one bridged call reads back.
const agentAPIResponseCap = 1 << 20

// agentAPIHeaders are the request headers a bridged call carries over, so it
// authenticates, resolves its workspace and is rate limited exactly as the caller.
var agentAPIHeaders = []string{
	"Authorization", "User-Agent", "Accept-Language", "Origin",
	"X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-Port", "X-Real-Ip",
}

// routerDispatcher replays an agent's API call through the backend's own router.
type routerDispatcher struct {
	router     http.Handler
	header     http.Header
	host       string
	remoteAddr string
}

// agentAPI is the caller's own API access for the bridge tools, or nil when the router is not wired.
func (h *Handler) agentAPI(c *gin.Context) aitools.APIDispatcher {
	if h.Router == nil || c.Request == nil {
		return nil
	}
	d := &routerDispatcher{router: h.Router, header: http.Header{}, host: c.Request.Host, remoteAddr: c.Request.RemoteAddr}
	for _, name := range agentAPIHeaders {
		for _, v := range c.Request.Header.Values(name) {
			d.header.Add(name, v)
		}
	}
	if d.header.Get("Authorization") == "" {
		return nil
	}
	return d
}

func (d *routerDispatcher) Do(ctx context.Context, method, path string, query url.Values, body []byte) (int, []byte, error) {
	u := url.URL{Path: "/v1" + path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header = d.header.Clone()
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Host = d.host
	req.RemoteAddr = d.remoteAddr
	w := &capturedResponse{header: http.Header{}, status: http.StatusOK}
	d.router.ServeHTTP(w, req)
	if w.overflow {
		return w.status, w.body.Bytes(), errors.New("the API response was too large; ask for fewer results with limit or filters")
	}
	return w.status, w.body.Bytes(), nil
}

// capturedResponse records a bridged response in memory, up to agentAPIResponseCap.
type capturedResponse struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	wrote    bool
	overflow bool
}

func (w *capturedResponse) Header() http.Header { return w.header }

func (w *capturedResponse) WriteHeader(status int) {
	if !w.wrote {
		w.status, w.wrote = status, true
	}
}

func (w *capturedResponse) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if room := agentAPIResponseCap - w.body.Len(); len(p) > room {
		w.body.Write(p[:max(room, 0)])
		w.overflow = true
		return len(p), nil
	}
	return w.body.Write(p)
}

// Flush satisfies http.Flusher for handlers that stream; the bytes are already buffered.
func (w *capturedResponse) Flush() {}
