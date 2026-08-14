package confenge

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	LedgerSchemaV1    = "confenge.ledger.v1"
	LedgerMaxAttempts = 5
	OutcomeUnknown    = "UNKNOWN"
	LedgerOrphan      = "orphan"
	LedgerConflict    = "conflict"
	LedgerDuplicate   = "duplicate"
	LedgerOutOfOrder  = "out_of_order"
	LedgerUnavailable = "ledger_unavailable"
)

// LedgerChain reconstructs Evidence → Decision → Target → Message → Action → Outcome → Evaluation.
type LedgerChain struct {
	SchemaVersion string   `json:"schema_version"`
	EvidenceIDs   []string `json:"evidence_ids,omitempty"`
	DecisionID    string   `json:"decision_id,omitempty"`
	TargetID      string   `json:"target_id,omitempty"`
	MessageID     string   `json:"message_id,omitempty"`
	ActionID      string   `json:"action_id"`
	OutcomeID     string   `json:"outcome_id,omitempty"`
	EvaluationID  string   `json:"evaluation_id,omitempty"`
}

// ActionRecord is one side effect Warmbly owns (HOW/EXECUTION).
type ActionRecord struct {
	ActionID       string    `json:"action_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Kind           string    `json:"kind"`
	TargetID       string    `json:"target_id,omitempty"`
	MessageID      string    `json:"message_id,omitempty"`
	DecisionID     string    `json:"decision_id,omitempty"`
	EvidenceIDs    []string  `json:"evidence_ids,omitempty"`
	PayloadHash    string    `json:"payload_hash,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
	Receipt        string    `json:"receipt,omitempty"`
	Attempts       int       `json:"attempts"`
	DLQ            bool      `json:"dlq,omitempty"`
}

// LedgerOutcome is a received execution result. WON is never inferred.
type LedgerOutcome struct {
	OutcomeID      string    `json:"outcome_id"`
	ActionID       string    `json:"action_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Type           string    `json:"type"`
	OccurredAt     time.Time `json:"occurred_at"`
	HumanConfirmed bool      `json:"human_confirmed,omitempty"`
	Receipt        string    `json:"receipt,omitempty"`
}

// LedgerException is an orphan/conflict/out-of-order item.
type LedgerException struct {
	Code       string    `json:"code"`
	Reason     string    `json:"reason"`
	NextAction string    `json:"next_action"`
	ActionID   string    `json:"action_id,omitempty"`
	OutcomeID  string    `json:"outcome_id,omitempty"`
	At         time.Time `json:"at"`
}

// MemoryLedger is an idempotent, HMAC-receipted in-process ledger for tests
// and the fail-closed execution path. extra-cli remains WHO/WHY NOW.
type MemoryLedger struct {
	mu          sync.Mutex
	secret      string
	actions     map[string]ActionRecord
	outcomes    map[string]LedgerOutcome
	byAction    map[string]string // action_id → outcome idempotency
	exceptions  []LedgerException
	unavailable bool
}

func NewMemoryLedger(secret string) *MemoryLedger {
	if secret == "" {
		secret = "confenge-ledger-dev"
	}
	return &MemoryLedger{
		secret:   secret,
		actions:  map[string]ActionRecord{},
		outcomes: map[string]LedgerOutcome{},
		byAction: map[string]string{},
	}
}

func (l *MemoryLedger) SetUnavailable(v bool) { l.unavailable = v }

func (l *MemoryLedger) RecordAction(a ActionRecord) (ActionRecord, *LedgerException) {
	if l == nil {
		return a, &LedgerException{Code: LedgerUnavailable, Reason: "ledger nil", NextAction: "retry", At: time.Now().UTC()}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.unavailable {
		return a, &LedgerException{Code: LedgerUnavailable, Reason: "ledger temporarily unavailable", NextAction: "retry with same idempotency key", At: time.Now().UTC()}
	}
	if strings.TrimSpace(a.IdempotencyKey) == "" {
		return a, &LedgerException{Code: LedgerConflict, Reason: "missing idempotency key", NextAction: "assign key", At: time.Now().UTC()}
	}
	if existing, ok := l.actions[a.IdempotencyKey]; ok {
		return existing, nil
	}
	if a.ActionID == "" {
		a.ActionID = uuid.NewString()
	}
	if a.OccurredAt.IsZero() {
		a.OccurredAt = time.Now().UTC()
	}
	a.Receipt = l.signLocked(a.IdempotencyKey + a.ActionID)
	l.actions[a.IdempotencyKey] = a
	return a, nil
}

func (l *MemoryLedger) RecordOutcome(o LedgerOutcome) (LedgerOutcome, *LedgerException) {
	if l == nil {
		return o, &LedgerException{Code: LedgerUnavailable, Reason: "ledger nil", NextAction: "retry", At: time.Now().UTC()}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.unavailable {
		return o, &LedgerException{Code: LedgerUnavailable, Reason: "ledger temporarily unavailable", NextAction: "retry with same idempotency key", At: time.Now().UTC()}
	}
	if strings.EqualFold(o.Type, OutcomeWon) && !o.HumanConfirmed {
		ex := LedgerException{Code: LedgerConflict, Reason: "WON cannot be inferred", NextAction: "require human/document confirmation", OutcomeID: o.OutcomeID, At: time.Now().UTC()}
		l.exceptions = append(l.exceptions, ex)
		return o, &ex
	}
	if strings.TrimSpace(o.IdempotencyKey) == "" {
		ex := LedgerException{Code: LedgerConflict, Reason: "missing outcome idempotency key", NextAction: "assign key", At: time.Now().UTC()}
		l.exceptions = append(l.exceptions, ex)
		return o, &ex
	}
	if existing, ok := l.outcomes[o.IdempotencyKey]; ok {
		return existing, nil
	}
	if o.ActionID != "" {
		if prev, ok := l.byAction[o.ActionID]; ok && prev != o.IdempotencyKey {
			ex := LedgerException{Code: LedgerConflict, Reason: "duplicate outcome for action", NextAction: "keep first receipt", ActionID: o.ActionID, At: time.Now().UTC()}
			l.exceptions = append(l.exceptions, ex)
			return l.outcomes[prev], &ex
		}
		if _, ok := l.actionsByIDLocked(o.ActionID); !ok {
			ex := LedgerException{Code: LedgerOrphan, Reason: "outcome without action", NextAction: "hold on exception queue until action arrives", OutcomeID: o.OutcomeID, At: time.Now().UTC()}
			l.exceptions = append(l.exceptions, ex)
			// Preserve out-of-order; do not invent causal order.
			if o.OutcomeID == "" {
				o.OutcomeID = uuid.NewString()
			}
			o.Receipt = l.signLocked(o.IdempotencyKey + o.OutcomeID)
			l.outcomes[o.IdempotencyKey] = o
			return o, &ex
		}
	}
	if o.OutcomeID == "" {
		o.OutcomeID = uuid.NewString()
	}
	if o.OccurredAt.IsZero() {
		o.OccurredAt = time.Now().UTC()
	}
	o.Receipt = l.signLocked(o.IdempotencyKey + o.OutcomeID)
	l.outcomes[o.IdempotencyKey] = o
	if o.ActionID != "" {
		l.byAction[o.ActionID] = o.IdempotencyKey
	}
	return o, nil
}

func (l *MemoryLedger) Replay(key string) (ActionRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.actions[key]
	return a, ok
}

func (l *MemoryLedger) RetryAction(key string) (ActionRecord, *LedgerException) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.actions[key]
	if !ok {
		return a, &LedgerException{Code: LedgerOrphan, Reason: "unknown action", NextAction: "re-record", At: time.Now().UTC()}
	}
	a.Attempts++
	if a.Attempts >= LedgerMaxAttempts {
		a.DLQ = true
		l.actions[key] = a
		ex := LedgerException{Code: "dlq", Reason: "retry budget exhausted", NextAction: "manual replay from DLQ", ActionID: a.ActionID, At: time.Now().UTC()}
		l.exceptions = append(l.exceptions, ex)
		return a, &ex
	}
	l.actions[key] = a
	return a, nil
}

func (l *MemoryLedger) Reconcile() (actions, outcomes, exceptions int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.actions), len(l.outcomes), len(l.exceptions)
}

func (l *MemoryLedger) Exceptions() []LedgerException {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LedgerException, len(l.exceptions))
	copy(out, l.exceptions)
	return out
}

func (l *MemoryLedger) Reconstruct(actionKey string) (LedgerChain, *LedgerException) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.actions[actionKey]
	if !ok {
		return LedgerChain{}, &LedgerException{Code: LedgerOrphan, Reason: "action missing", NextAction: "replay", At: time.Now().UTC()}
	}
	ch := LedgerChain{
		SchemaVersion: LedgerSchemaV1,
		EvidenceIDs:   append([]string{}, a.EvidenceIDs...),
		DecisionID:    a.DecisionID,
		TargetID:      a.TargetID,
		MessageID:     a.MessageID,
		ActionID:      a.ActionID,
	}
	if ik, ok := l.byAction[a.ActionID]; ok {
		if o, ok := l.outcomes[ik]; ok {
			ch.OutcomeID = o.OutcomeID
		}
	}
	return ch, nil
}

func (l *MemoryLedger) signLocked(material string) string {
	mac := sha256.Sum256([]byte(l.secret + ":" + material))
	return hex.EncodeToString(mac[:])
}

func (l *MemoryLedger) actionsByIDLocked(id string) (ActionRecord, bool) {
	for _, a := range l.actions {
		if a.ActionID == id {
			return a, true
		}
	}
	return ActionRecord{}, false
}

// HashMaterial is a stable hash of sorted parts (feed, cohort, approvals).
func HashMaterial(parts ...string) string {
	cp := append([]string{}, parts...)
	sort.Strings(cp)
	h := sha256.Sum256([]byte(strings.Join(cp, "|")))
	return hex.EncodeToString(h[:])
}
