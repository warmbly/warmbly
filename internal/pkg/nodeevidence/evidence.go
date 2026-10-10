// Package nodeevidence transports fixed-shape operational evidence, never arbitrary log text.
package nodeevidence

import (
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	ControlPlaneHeld    = "sync_control_plane_held"
	ProviderUnreachable = "mailbox_provider_unreachable"
	ProviderAuth        = "mailbox_provider_auth_failed"
	ProviderThrottled   = "mailbox_provider_throttled"
	ProviderRecovered   = "mailbox_provider_sync_completed"
	FolderSkipped       = "sync_folder_skipped"
	SearchSkipped       = "sync_skipped_folder_search_failed"
	MessageDeferred     = "sync_message_deferred"
	PolicyDeferred      = "sync_message_deferred_by_policy"
	LegacyArrival       = "sync_legacy_arrival"
	WorkerError         = "worker_error"
)

type Event struct {
	ID         uuid.UUID `json:"id"`
	ObservedAt time.Time `json:"observed_at"`
	Name       string    `json:"event"`
	Level      string    `json:"level"`
	Category   string    `json:"category"`
	MailboxID  uuid.UUID `json:"mailbox_id,omitempty"`
	HTTPStatus int       `json:"http_status,omitempty"`
	Count      int       `json:"count,omitempty"`
}

// Sanitize is used on both sides of transport; there are no free-form strings to scrub heuristically.
func Sanitize(e Event) (Event, bool) {
	switch e.Name {
	case ControlPlaneHeld, MessageDeferred:
		e.Level, e.Category = "warn", "control_plane"
	case ProviderUnreachable, ProviderAuth, ProviderThrottled:
		e.Level, e.Category = "warn", "provider"
	case ProviderRecovered:
		e.Level, e.Category = "info", "provider"
	case FolderSkipped, PolicyDeferred:
		e.Level, e.Category = "info", "sync_policy"
	case SearchSkipped:
		e.Level, e.Category = "warn", "provider"
	case LegacyArrival:
		e.Level, e.Category = "warn", "compatibility"
	case WorkerError:
		e.Level, e.Category = "error", "worker_runtime"
	default:
		return Event{}, false
	}
	if e.HTTPStatus < 100 || e.HTTPStatus > 599 {
		e.HTTPStatus = 0
	}
	if e.Name != ControlPlaneHeld {
		e.HTTPStatus = 0
	}
	if e.Count < 0 || e.Count > 1000000 {
		e.Count = 0
	}
	return e, true
}

type recorder struct{ fn func(Event) }

var sink atomic.Pointer[recorder]

func Install(fn func(Event)) { sink.Store(&recorder{fn: fn}) }

func Emit(name string, mailbox uuid.UUID, status, count int) {
	r := sink.Load()
	if r == nil || r.fn == nil {
		return
	}
	e, ok := Sanitize(Event{ID: uuid.New(), ObservedAt: time.Now().UTC(), Name: name, MailboxID: mailbox, HTTPStatus: status, Count: count})
	if ok {
		r.fn(e)
	}
}

type Batch struct {
	Protocol   int       `json:"protocol"`
	BatchID    uuid.UUID `json:"batch_id"`
	RunID      uuid.UUID `json:"run_id"`
	StartedAt  time.Time `json:"started_at"`
	ObservedAt time.Time `json:"observed_at"`
	Dropped    uint64    `json:"dropped"`
	Events     []Event   `json:"events"`
}
