package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
)

type logCapture struct {
	queue   chan nodeevidence.Event
	dropped atomic.Uint64
	runID   uuid.UUID
	started time.Time
}

func (l *logCapture) record(e nodeevidence.Event) {
	e, ok := nodeevidence.Sanitize(e)
	if !ok {
		l.dropped.Add(1)
		return
	}
	select {
	case l.queue <- e:
	default:
		l.dropped.Add(1)
	}
}

func (a *Agent) prepareLogs() {
	// Only an enrollment-issued node credential can enable capture.
	if a.cfg.LogToken == "" {
		a.cfg.LogToken = os.Getenv("NODE_LOG_TOKEN")
	}
	if a.cfg.LogToken == "" {
		return
	}
	a.logs = &logCapture{queue: make(chan nodeevidence.Event, 256), runID: uuid.New(), started: time.Now().UTC()}
	nodeevidence.Install(a.logs.record)
}

func (a *Agent) runLogs(ctx context.Context) {
	if a.logs == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		batch := nodeevidence.Batch{Protocol: 1, BatchID: uuid.New(), RunID: a.logs.runID, StartedAt: a.logs.started, ObservedAt: time.Now().UTC(), Dropped: a.logs.dropped.Load(), Events: []nodeevidence.Event{}}
	drain:
		for len(batch.Events) < 32 {
			select {
			case e := <-a.logs.queue:
				batch.Events = append(batch.Events, e)
			default:
				break drain
			}
		}
		body, err := json.Marshal(batch)
		if err != nil {
			a.logs.dropped.Add(uint64(len(batch.Events)))
			continue
		}
		ok := false
		for attempt := range 3 {
			if attempt > 0 {
				timer := time.NewTimer(time.Duration(attempt) * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			result := a.sendLogs(ctx, body)
			if result.accepted {
				ok = true
				break
			}
			if !result.retryable {
				if result.cooldown > 0 {
					timer := time.NewTimer(result.cooldown)
					select {
					case <-ctx.Done():
						timer.Stop()
						a.logs.dropped.Add(uint64(len(batch.Events)))
						return
					case <-timer.C:
					}
				}
				break
			}
		}
		if !ok {
			a.logs.dropped.Add(uint64(len(batch.Events)))
		}
	}
}

type logSendResult struct {
	accepted, retryable bool
	cooldown            time.Duration
}

func (a *Agent) sendLogs(ctx context.Context, body []byte) logSendResult {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := strings.TrimRight(a.cfg.BaseURL, "/") + "/api/v1/internal/fleet/nodes/" + a.cfg.NodeID.String() + "/logs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return logSendResult{}
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.LogToken)
	req.Header.Set("Content-Type", "application/json")
	// Never follow a redirect with a node credential or expose transport errors in evidence.
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return logSendResult{retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		cooldown := time.Minute
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 && seconds <= 300 {
			cooldown = time.Duration(seconds) * time.Second
		}
		return logSendResult{cooldown: cooldown}
	}
	return logSendResult{accepted: resp.StatusCode == http.StatusNoContent, retryable: resp.StatusCode >= 500}
}
