package nodeagent

import (
	"math"
	"sync"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	// memoryTargetShare is the share of the machine a worker plans to fill;
	// the rest is headroom for bursts, the garbage collector and the host.
	memoryTargetShare = 0.70
	// defaultMailboxMB is assumed until enough mailboxes are loaded to measure.
	defaultMailboxMB = 1.0
	// minMailboxSample is how many mailboxes a measurement needs.
	minMailboxSample = 50
	capacityFloor    = 50
	capacityCeiling  = 100_000
)

// MemoryCapacity sizes a worker's mailbox target from its own memory: what the
// machine has, and what each mailbox it holds has been measured to cost.
type MemoryCapacity struct {
	// Mailboxes is how many mailboxes the worker holds now.
	Mailboxes func() int

	mu         sync.Mutex
	baseline   float64
	perMailbox float64
}

// Target is the mailbox count that fills memoryTargetShare of the machine, or
// zero when the beat carries no memory reading, which leaves the stored target.
func (m *MemoryCapacity) Target(u models.NodeUsage) float64 {
	if u.MemoryLimitMB == nil || *u.MemoryLimitMB <= 0 || u.ResidentMB == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	resident, limit := float64(*u.ResidentMB), float64(*u.MemoryLimitMB)
	held := 0
	if m.Mailboxes != nil {
		held = m.Mailboxes()
	}
	// The process's own footprint with no mailboxes: the first beat, before any load.
	if m.baseline == 0 || held == 0 && resident < m.baseline {
		m.baseline = resident
	}
	if held >= minMailboxSample && resident > m.baseline {
		sample := (resident - m.baseline) / float64(held)
		if m.perMailbox == 0 {
			m.perMailbox = sample
		} else {
			// Smoothed so one beat after a burst does not swing placement.
			m.perMailbox += 0.2 * (sample - m.perMailbox)
		}
	}
	per := defaultMailboxMB
	if m.perMailbox > 0 {
		per = max(m.perMailbox, 0.05)
	}
	budget := limit*memoryTargetShare - m.baseline
	if budget <= 0 {
		return capacityFloor
	}
	return min(max(math.Floor(budget/per), capacityFloor), capacityCeiling)
}
