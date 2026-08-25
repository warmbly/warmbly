package plathealth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
)

// ProbeConfig is the shipped external-probe entry. Fixture mode is the
// testable path. Live mode talks to a base URL (and optional NATS URL).
type ProbeConfig struct {
	BaseURL     string
	NATSURL     string
	FixturePath string
	Timeout     time.Duration
	// Optional injections. Nil uses net/http and net.DefaultResolver.
	LookupHost func(ctx context.Context, host string) ([]string, error)
	TLSDial    func(ctx context.Context, network, addr string, cfg *tls.Config) error
	HTTPClient *http.Client
	Bus        eventbus.EventBus
	Now        func() time.Time
}

// RunProbe is the shipped probe entry point. Tests drive this function.
func RunProbe(ctx context.Context, cfg ProbeConfig) (ProbeReport, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.FixturePath != "" {
		f, err := os.Open(cfg.FixturePath)
		if err != nil {
			return ProbeReport{}, err
		}
		defer f.Close()
		in, err := DecodeProbeInput(f)
		if err != nil {
			return ProbeReport{}, err
		}
		if in.CheckedAt.IsZero() {
			in.CheckedAt = cfg.Now()
		}
		return EvaluateProbe(in), nil
	}
	in, err := collectLive(ctx, cfg)
	if err != nil {
		return ProbeReport{}, err
	}
	in.CheckedAt = cfg.Now()
	return EvaluateProbe(in), nil
}

func collectLive(ctx context.Context, cfg ProbeConfig) (ProbeInput, error) {
	var in ProbeInput
	if strings.TrimSpace(cfg.BaseURL) == "" && cfg.Bus == nil && cfg.NATSURL == "" {
		return in, fmt.Errorf("base-url, nats-url, or fixture is required")
	}

	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return in, fmt.Errorf("invalid base-url")
		}
		host := u.Hostname()
		in.DNS = liveDNS(ctx, cfg, host)
		if u.Scheme == "https" {
			port := u.Port()
			if port == "" {
				port = "443"
			}
			in.TLS = liveTLS(ctx, cfg, net.JoinHostPort(host, port), host)
		} else {
			in.TLS = CheckInput{Observed: false, Reason: ReasonHTTPScheme}
		}
		var deps *depsBody
		in.API, deps = liveAPI(ctx, cfg, strings.TrimRight(cfg.BaseURL, "/"))
		liftDeps(&in, deps)
	} else {
		in.DNS = CheckInput{Observed: false, Reason: ReasonUnobserved}
		in.TLS = CheckInput{Observed: false, Reason: ReasonUnobserved}
		in.API = APIInput{Observed: false}
	}

	if cfg.Bus != nil || cfg.NATSURL != "" {
		bus := cfg.Bus
		var closer io.Closer
		if bus == nil {
			b, err := eventbus.NewNATS(eventbus.NATSConfig{URL: cfg.NATSURL})
			if err != nil {
				in.EventIngest = CheckInput{Observed: true, OK: false, Reason: ReasonPublishFailed}
				in.ReadAfterWrite = CheckInput{Observed: true, OK: false, Reason: ReasonSubscribeFailed}
				return in, nil
			}
			bus = b
			closer = b
		}
		if closer != nil {
			defer closer.Close()
		}
		cctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		ingest, raw := ProbeBusRoundTrip(cctx, bus)
		cancel()
		in.EventIngest = CheckInput{Observed: ingest.Observed, OK: ingest.OK, Timeout: ingest.Timeout, Reason: ingest.Reason, LatencyMS: ingest.LatencyMS}
		in.ReadAfterWrite = CheckInput{Observed: raw.Observed, OK: raw.OK, Timeout: raw.Timeout, Reason: raw.Reason, LatencyMS: raw.LatencyMS}
	}
	return in, nil
}

func liveDNS(ctx context.Context, cfg ProbeConfig, host string) CheckInput {
	lookup := cfg.LookupHost
	if lookup == nil {
		var r net.Resolver
		lookup = r.LookupHost
	}
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	addrs, err := lookup(cctx, host)
	out := CheckInput{Observed: true, LatencyMS: time.Since(start).Milliseconds()}
	if err != nil || len(addrs) == 0 {
		out.OK = false
		out.Reason = ReasonDNSFailed
		if cctx.Err() != nil {
			out.Timeout = true
			out.Reason = ReasonTimeout
		}
		return out
	}
	out.OK = true
	return out
}

func liveTLS(ctx context.Context, cfg ProbeConfig, addr, serverName string) CheckInput {
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	out := CheckInput{Observed: true, LatencyMS: time.Since(start).Milliseconds()}
	tlsCfg := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}
	dial := cfg.TLSDial
	if dial == nil {
		dial = func(ctx context.Context, network, address string, cfg *tls.Config) error {
			d := &tls.Dialer{Config: cfg}
			conn, err := d.DialContext(ctx, network, address)
			if err != nil {
				return err
			}
			return conn.Close()
		}
	}
	if err := dial(cctx, "tcp", addr, tlsCfg); err != nil {
		out.OK = false
		out.Reason = ReasonTLSFailed
		out.LatencyMS = time.Since(start).Milliseconds()
		if cctx.Err() != nil {
			out.Timeout = true
			out.Reason = ReasonTimeout
		}
		return out
	}
	out.OK = true
	out.LatencyMS = time.Since(start).Milliseconds()
	return out
}

type depsBody struct {
	Ready  bool `json:"ready"`
	Live   bool `json:"live"`
	Planes []struct {
		Plane  string `json:"plane"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"planes"`
}

func liveAPI(ctx context.Context, cfg ProbeConfig, base string) (APIInput, *depsBody) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	start := time.Now()
	out := APIInput{Observed: true, LatencyMS: time.Since(start).Milliseconds()}
	out.HealthStatus = getStatus(ctx, client, base+"/health")
	out.LiveStatus = getStatus(ctx, client, base+"/live")
	readyStatus, readyBody := getJSON(ctx, client, base+"/ready")
	out.ReadyStatus = readyStatus
	if readyBody != nil {
		out.Ready = &readyBody.Ready
	}
	if out.HealthStatus == 200 && out.LiveStatus == 0 && out.Ready == nil {
		out.ProcessUpOnly = true
	}
	out.LatencyMS = time.Since(start).Milliseconds()
	return out, readyBody
}

func getStatus(ctx context.Context, client *http.Client, rawURL string) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode
}

func getJSON(ctx context.Context, client *http.Client, rawURL string) (int, *depsBody) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var body depsBody
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, &body
}

func liftDeps(in *ProbeInput, body *depsBody) {
	if body == nil {
		if !in.EventIngest.Observed {
			in.EventIngest = CheckInput{Observed: false, Reason: ReasonUnobserved}
		}
		if !in.ReadAfterWrite.Observed {
			in.ReadAfterWrite = CheckInput{Observed: false, Reason: ReasonUnobserved}
		}
		if !in.WorkerHeartbeat.Observed {
			in.WorkerHeartbeat = CheckInput{Observed: false, Reason: ReasonUnobserved}
		}
		return
	}
	sawQueue := false
	sawEventProcessing := false
	sawHeartbeat := false
	for _, p := range body.Planes {
		ci := planeToCheck(p.Status, p.Reason)
		switch p.Plane {
		case PlaneQueue:
			sawQueue = true
			in.EventIngest = ci
		case PlaneEventProcessing:
			sawEventProcessing = true
			in.ReadAfterWrite = ci
		case PlaneWorkerHeartbeat:
			sawHeartbeat = true
			in.WorkerHeartbeat = ci
		}
	}
	if !sawQueue {
		in.EventIngest = CheckInput{Reason: ReasonContractMissingPlane}
	}
	if !sawEventProcessing {
		in.ReadAfterWrite = CheckInput{Reason: ReasonContractMissingPlane}
	}
	if !sawHeartbeat {
		in.WorkerHeartbeat = CheckInput{Reason: ReasonContractMissingPlane}
	}
}

func planeToCheck(status, reason string) CheckInput {
	ci := CheckInput{Observed: status != StatusUnobserved, Reason: AllowReason(reason)}
	switch status {
	case StatusOK:
		ci.Observed = true
		ci.OK = true
	case StatusTimeout:
		ci.Timeout = true
	case StatusStale:
		ci.Stale = true
	case StatusUnobserved:
		ci.Observed = false
	default:
		ci.OK = false
	}
	return ci
}
