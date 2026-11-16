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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
)

// DefaultInterval is used until the control plane says otherwise. The first
// reply carries the server's liveness window and the agent re-paces itself to
// a third of it, so the two can never drift apart.
const DefaultInterval = 90 * time.Second

type Config struct {
	LogToken           string
	WarmupSendProtocol int
	SyncWakeProtocol   int
	NodeID             uuid.UUID
	Role               models.NodeRole
	Name               string
	Region             string
	Address            string
	// CapacityTarget is meaningful for workers only. Zero leaves the control
	// plane's stored/default target unchanged.
	CapacityTarget float64
	// Capacity, when set, replaces CapacityTarget with a target derived from
	// each beat's usage; a zero result keeps CapacityTarget.
	Capacity func(models.NodeUsage) float64
	// Version is what this build reports. Empty means "unknown", which the
	// control plane must not read as "needs updating".
	Version string

	BaseURL string
	Token   string
	// Credential is the node credential join issued to NodeID; empty sends the fleet token alone.
	Credential string

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
	logs           *logCapture
	cfg            Config
	http           *http.Client
	started        time.Time
	usage          usageSampler
	address        string
	addressChecked time.Time

	// lastErr is reported on the next beat and then cleared, so the dashboard
	// shows what went wrong without it sticking forever.
	lastErr atomic.Pointer[string]

	missingMu sync.Mutex
	missing   map[uuid.UUID]struct{}
	// reloadsByID is the last reply's word that the backend re-ships named mailboxes; until then a miss asks for a full reload.
	reloadsByID atomic.Bool
}

// maxMissingPerBeat bounds one beat's list; the rest wait for the next beat.
const maxMissingPerBeat = 500

func New(cfg Config) *Agent {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	a := &Agent{cfg: cfg, http: cfg.HTTPClient, started: time.Now()}
	a.prepareLogs()
	return a
}

// ReportError attaches a message to the next heartbeat. Safe from any
// goroutine; the newest message wins.
func (a *Agent) ReportError(msg string) {
	a.lastErr.Store(&msg)
}

// RequestReload asks the next acknowledged heartbeat to have the backend re-ship a mailbox this node does not hold.
func (a *Agent) RequestReload(mailbox uuid.UUID) {
	a.missingMu.Lock()
	defer a.missingMu.Unlock()
	if a.missing == nil {
		a.missing = make(map[uuid.UUID]struct{})
	}
	a.missing[mailbox] = struct{}{}
}

// takeMissing drains up to limit requested mailboxes, all of them when limit is 0.
func (a *Agent) takeMissing(limit int) []uuid.UUID {
	a.missingMu.Lock()
	defer a.missingMu.Unlock()
	out := make([]uuid.UUID, 0, len(a.missing))
	for id := range a.missing {
		if limit > 0 && len(out) == limit {
			break
		}
		out = append(out, id)
		delete(a.missing, id)
	}
	return out
}

// returnMissing puts back the mailboxes a beat failed to deliver.
func (a *Agent) returnMissing(ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	a.missingMu.Lock()
	defer a.missingMu.Unlock()
	if a.missing == nil {
		a.missing = make(map[uuid.UUID]struct{})
	}
	for _, id := range ids {
		a.missing[id] = struct{}{}
	}
}

// Run beats until ctx is cancelled, then sends one farewell beat so the node
// goes inactive immediately rather than staying selectable until it ages out.
//
// A failed beat is logged and retried on the next tick. It is never fatal: a
// node that cannot reach the control plane should keep doing the work it
// already has, not stop.
func (a *Agent) Run(ctx context.Context) {
	if a.cfg.BaseURL == "" || (a.cfg.Token == "" && a.cfg.Credential == "") {
		log.Println("nodeagent: no backend URL or token; heartbeats disabled")
		return
	}
	go a.runLogs(ctx)

	interval := DefaultInterval
	bootPending := true
	if reply := a.reloadBeat(ctx, bootPending); reply != nil {
		bootPending = false
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
			if reply := a.reloadBeat(ctx, bootPending); reply != nil {
				bootPending = false
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

// reloadBeat beats with the boot flag and the mailboxes asked for since the last acknowledged beat.
// A backend that has not said it reloads by id gets the boot flag instead, its only way to re-ship.
func (a *Agent) reloadBeat(ctx context.Context, bootPending bool) *models.NodeHeartbeatReply {
	byID := a.reloadsByID.Load()
	booted := bootPending
	var missing, named []uuid.UUID
	if byID {
		missing = a.takeMissing(maxMissingPerBeat)
		named = missing
	} else if missing = a.takeMissing(0); len(missing) > 0 {
		// One full reload covers every pending mailbox.
		booted = true
	}
	reply := a.beatWith(ctx, booted, false, named)
	// A lost beat, or names sent to a backend that no longer reloads by id (a rollback), are asked for again.
	if reply == nil || len(named) > 0 && !reply.ReloadsByID {
		a.returnMissing(missing)
	}
	return reply
}

func (a *Agent) beat(ctx context.Context, booted, stopping bool) *models.NodeHeartbeatReply {
	return a.beatWith(ctx, booted, stopping, nil)
}

func (a *Agent) beatWith(ctx context.Context, booted, stopping bool, missing []uuid.UUID) *models.NodeHeartbeatReply {
	usage := a.sampleUsage()
	capacity := a.cfg.CapacityTarget
	if a.cfg.Capacity != nil {
		if v := a.cfg.Capacity(usage); v > 0 {
			capacity = v
		}
	}
	beat := models.NodeHeartbeat{
		WarmupSendProtocol: a.cfg.WarmupSendProtocol,
		SyncWakeProtocol:   a.cfg.SyncWakeProtocol,
		NodeID:             a.cfg.NodeID,
		Role:               a.cfg.Role,
		Name:               a.cfg.Name,
		Region:             a.cfg.Region,
		Address:            a.address,
		CapacityTarget:     capacity,
		Version:            a.cfg.Version,
		Usage:              usage,
		Booted:             booted,
		MissingMailboxes:   missing,
		Stopping:           stopping,
	}
	if !stopping {
		a.readUpdaterState(&beat)
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
	a.authorize(req)
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

	var reply *models.NodeHeartbeatReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil || reply == nil {
		return nil
	}
	a.reloadsByID.Store(reply.ReloadsByID)
	a.applyTarget(reply.DesiredVersion)
	a.applyDigest(reply.DesiredVersion, reply.DesiredDigest)
	a.applyEnvRevision(reply.EnvRevision)
	return reply
}

// authorize presents the fleet token and, when join issued one, this node's own credential.
func (a *Agent) authorize(req *http.Request) {
	if a.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	}
	if a.cfg.Credential != "" {
		req.Header.Set(nodeauth.IDHeader, a.cfg.NodeID.String())
		req.Header.Set(nodeauth.CredentialHeader, a.cfg.Credential)
	}
}

// The host updater shares the target directory: it writes update-failed and
// env-revision there, and reads target-digest and target-env-revision.
func (a *Agent) stateFile(name string) string {
	if a.cfg.TargetVersionPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(a.cfg.TargetVersionPath), name)
}

func (a *Agent) readUpdaterState(beat *models.NodeHeartbeat) {
	if a.cfg.TargetVersionPath == "" {
		return
	}
	beat.UpdateFailed = readToken(a.stateFile("update-failed"))
	beat.EnvRefresh = os.Getenv("WARMBLY_ENV_REFRESH") == "true"
	if beat.EnvRefresh {
		beat.EnvRevision = readToken(a.stateFile("env-revision"))
	}
}

// readToken returns the first line of a small state file, or "" when it is
// absent or holds anything but a plain token.
func readToken(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 256 {
		return ""
	}
	v := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".-_+:", c)) {
			return ""
		}
	}
	return v
}

// applyDigest records the digest a staged rollout pins the target to. It is
// written as "<tag> <digest>" so the updater uses it only for that tag.
func (a *Agent) applyDigest(version, digest string) {
	path := a.stateFile("target-digest")
	if path == "" {
		return
	}
	if version == "" || digest == "" {
		_ = os.Remove(path)
		return
	}
	writeAtomic(path, version+" "+digest+"\n")
}

func (a *Agent) applyEnvRevision(rev string) {
	path := a.stateFile("target-env-revision")
	if path == "" || rev == "" || os.Getenv("WARMBLY_ENV_REFRESH") != "true" {
		return
	}
	if readToken(path) == rev {
		return
	}
	writeAtomic(path, rev+"\n")
}

func writeAtomic(path, body string) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		log.Println("nodeagent: cannot write updater state:", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Println("nodeagent: cannot publish updater state:", err)
	}
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
