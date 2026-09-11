// Package sysstatus runs cheap liveness probes against the platform's backing
// services (Postgres, Redis, Kafka, schema registry, realtime, ...) so the
// admin dashboard can show component health at a glance. Checks are wired as
// closures in cmd/backend/main.go where the concrete clients live; this
// package only knows how to run them in parallel with a bounded timeout.
package sysstatus

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// perCheckTimeout bounds each probe so one dead dependency can't stall the
// whole status response.
const perCheckTimeout = 3 * time.Second

type check struct {
	name string
	fn   func(ctx context.Context) error
}

// Checker holds the registered probes.
type Checker struct {
	checks []check
}

// New creates an empty checker.
func New() *Checker {
	return &Checker{}
}

// Add registers a named probe. Nil funcs are ignored.
func (c *Checker) Add(name string, fn func(ctx context.Context) error) {
	if fn == nil {
		return
	}
	c.checks = append(c.checks, check{name: name, fn: fn})
}

// Result is one probe outcome.
type Result struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// Run executes every probe in parallel and returns results in registration
// order.
func (c *Checker) Run(ctx context.Context) []Result {
	results := make([]Result, len(c.checks))
	var wg sync.WaitGroup
	for i, ch := range c.checks {
		wg.Add(1)
		go func(i int, ch check) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, perCheckTimeout)
			defer cancel()
			start := time.Now()
			err := ch.fn(cctx)
			r := Result{Name: ch.name, OK: err == nil, LatencyMS: time.Since(start).Milliseconds()}
			if err != nil {
				r.Error = err.Error()
			}
			results[i] = r
		}(i, ch)
	}
	wg.Wait()
	return results
}

// HTTPCheck probes a URL and treats any response below 500 as healthy (auth
// walls still prove the service is up).
func HTTPCheck(url string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}
}

// TCPCheck probes the first address of a comma-separated list. Each entry may
// be a bare host:port or a full URL, because the variables these come from
// (NATS_URL, the broker list) are connection strings, not dial addresses.
func TCPCheck(addrs string) func(ctx context.Context) error {
	addr := DialAddr(strings.Split(addrs, ",")[0])
	return func(ctx context.Context) error {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		return conn.Close()
	}
}

// DialAddr reduces a connection string to the host:port net.Dial accepts.
// Credentials in the authority are the reason this exists: net.Dial reads
// "user:pass@host:4222" as an address with too many colons, so a credentialed
// NATS_URL reported the bus down while everything worked.
func DialAddr(raw string) string {
	addr := strings.TrimSpace(raw)
	if i := strings.Index(addr, "://"); i >= 0 {
		addr = addr[i+3:]
	}
	// Last "@": a password may legitimately contain one.
	if i := strings.LastIndex(addr, "@"); i >= 0 {
		addr = addr[i+1:]
	}
	// Anything after the authority (path, query, fragment) is not dialled.
	if i := strings.IndexAny(addr, "/?#"); i >= 0 {
		addr = addr[:i]
	}
	return addr
}
