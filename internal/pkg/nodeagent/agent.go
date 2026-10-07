// Package nodeagent is the node half of the pull-based fleet.
//
// Every Warmbly process that runs on a machine you own embeds it. The agent
// heartbeats to the control plane, reports what it is running and what it is
// using, and reads back the version it should be running. It never applies an
// update itself: replacing a running container from inside that container is
// how you get a process that cannot finish the job. Instead it writes the
// target where the host-side updater (a systemd timer installed by `warmbly
// join`) can see it, and that restarts the service.
package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// DefaultInterval is used until the control plane says otherwise. The first
// reply carries the server's liveness window and the agent re-paces itself to
// a third of it, so the two can never drift apart.
const DefaultInterval = 90 * time.Second

type Config struct {
	WarmupSendProtocol int
	NodeID             uuid.UUID
	Role               models.NodeRole
	Name               string
	Region             string
	Address            string
	// CapacityTarget is meaningful for workers only. Zero leaves the control
	// plane's stored/default target unchanged.
	CapacityTarget float64
	// Version is what this build reports. Empty means "unknown", which the
	// control plane must not read as "needs updating".
	Version string

	BaseURL string
	Token   string

	// TargetVersionPath is where the resolved target is written for the
	// host-side updater to read. Empty disables that, which is what you want
	// in dev where nothing supervises the process.
	TargetVersionPath string

	// Condition is a standing problem reported on every beat while it lasts,
	// unlike ReportError, which is reported once. Nil or empty reports none.
	Condition func() string

	HTTPClient *http.Client
}

type Agent struct {
	cfg            Config
	http           *http.Client
	started        time.Time
	usage          usageSampler
	address        string
	addressChecked time.Time

	// lastErr is reported on the next beat and then cleared, so the dashboard
	// shows what went wrong without it sticking forever.
	lastErr atomic.Pointer[string]
}

func New(cfg Config) *Agent {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Agent{cfg: cfg, http: cfg.HTTPClient, started: time.Now()}
}

// ReportError attaches a message to the next heartbeat. Safe from any
// goroutine; the newest message wins.
func (a *Agent) ReportError(msg string) {
	a.lastErr.Store(&msg)
}

// Run beats until ctx is cancelled, then sends one farewell beat so the node
// goes inactive immediately rather than staying selectable until it ages out.
//
// A failed beat is logged and retried on the next tick. It is never fatal: a
// node that cannot reach the control plane should keep doing the work it
// already has, not stop.
func (a *Agent) Run(ctx context.Context) {
	if a.cfg.BaseURL == "" || a.cfg.Token == "" {
		log.Println("nodeagent: no backend URL or token; heartbeats disabled")
		return
	}

	interval := DefaultInterval
	if reply := a.beat(ctx, true, false); reply != nil {
		interval = paceFrom(reply.LivenessSeconds)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Fresh context: ctx is already cancelled, and without the
			// farewell the row stays selectable until the beat ages out, so
			// the control plane keeps handing work to a process that has exited.
			byeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			a.beat(byeCtx, false, true)
			cancel()
			return
		case <-ticker.C:
			if reply := a.beat(ctx, false, false); reply != nil {
				if next := paceFrom(reply.LivenessSeconds); next != interval {
					interval = next
					ticker.Reset(interval)
				}
			}
		}
	}
}

// paceFrom beats three times per liveness window, so two lost beats in a row
// still do not look like a dead machine.
func paceFrom(livenessSeconds int) time.Duration {
	if livenessSeconds <= 0 {
		return DefaultInterval
	}
	d := time.Duration(livenessSeconds) * time.Second / 3
	if d < 15*time.Second {
		return 15 * time.Second
	}
	return d
}

func (a *Agent) beat(ctx context.Context, booted, stopping bool) *models.NodeHeartbeatReply {
	beat := models.NodeHeartbeat{
		WarmupSendProtocol: a.cfg.WarmupSendProtocol,
		NodeID:             a.cfg.NodeID,
		Role:               a.cfg.Role,
		Name:               a.cfg.Name,
		Region:             a.cfg.Region,
		Address:            a.address,
		CapacityTarget:     a.cfg.CapacityTarget,
		Version:            a.cfg.Version,
		Usage:              a.sampleUsage(),
		Booted:             booted,
		Stopping:           stopping,
	}
	if !stopping {
		beat.Address = a.publicAddress(ctx)
	}
	if p := a.lastErr.Swap(nil); p != nil {
		beat.LastError = *p
	} else if a.cfg.Condition != nil {
		beat.LastError = a.cfg.Condition()
	}

	body, err := json.Marshal(beat)
	if err != nil {
		return nil
	}
	url := strings.TrimRight(a.cfg.BaseURL, "/") + "/api/v1/internal/fleet/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Println("nodeagent: build heartbeat:", err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		log.Println("nodeagent: heartbeat failed:", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Println("nodeagent: heartbeat returned status", resp.StatusCode)
		return nil
	}

	var reply models.NodeHeartbeatReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil
	}
	a.applyTarget(reply.DesiredVersion)
	return &reply
}

// applyTarget records the version the control plane wants. Writing a file the
// host-side updater polls keeps the decision (control plane) separate from the
// mechanism (systemd timer), which is what lets a node replace itself without
// asking a process to kill and restart its own container.
//
// An empty target is ignored rather than written: "no opinion" must never
// become "downgrade to nothing".
func (a *Agent) applyTarget(version string) {
	if version == "" || a.cfg.TargetVersionPath == "" || version == a.cfg.Version {
		return
	}
	dir := filepath.Dir(a.cfg.TargetVersionPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Println("nodeagent: cannot create target dir:", err)
		return
	}
	tmp := a.cfg.TargetVersionPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(version+"\n"), 0o644); err != nil {
		log.Println("nodeagent: cannot write target version:", err)
		return
	}
	// Rename so the updater never reads a half-written file.
	if err := os.Rename(tmp, a.cfg.TargetVersionPath); err != nil {
		log.Println("nodeagent: cannot publish target version:", err)
		return
	}
	log.Printf("nodeagent: control plane wants %s (running %s); the host updater will apply it",
		version, a.cfg.Version)
}

func (a *Agent) sampleUsage() models.NodeUsage {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mem := int(m.Sys / 1024 / 1024)
	goroutines := runtime.NumGoroutine()
	uptime := int64(time.Since(a.started).Seconds())
	usage := models.NodeUsage{
		MemoryMB:      &mem,
		Goroutines:    &goroutines,
		UptimeSeconds: &uptime,
	}
	a.usage.sample(&usage)
	return usage
}
