// Package warmpersona derives a stable, per-mailbox "personality" from the
// mailbox account ID. The goal is to make warmup traffic from different
// mailboxes look behaviourally distinct (different voice, dwell, engagement
// propensity) instead of every mailbox in the pool behaving identically — a
// uniform pool is a fingerprint. The persona is deterministic (same mailbox →
// same persona) and requires no stored state.
package warmpersona

import (
	"hash/fnv"

	"github.com/google/uuid"
)

// Persona is a deterministic source of per-mailbox biases.
type Persona struct {
	seed uint64
}

// For derives the persona for a mailbox account ID.
func For(id uuid.UUID) Persona {
	h := fnv.New64a()
	_, _ = h.Write(id[:])
	return Persona{seed: h.Sum64()}
}

// axisValue returns a stable uint64 derived from the persona seed and a named
// axis, so independent axes (greeting, dwell, importance...) don't correlate.
func (p Persona) axisValue(axis string) uint64 {
	h := fnv.New64a()
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(p.seed >> (8 * i))
	}
	_, _ = h.Write(b[:])
	_, _ = h.Write([]byte(axis))
	return h.Sum64()
}

// unit returns a deterministic float in [0,1) for the given axis.
func (p Persona) unit(axis string) float64 {
	// Top 53 bits → float64 in [0,1), matching math/rand's Float64 precision.
	return float64(p.axisValue(axis)>>11) / float64(uint64(1)<<53)
}

// Bias returns a deterministic multiplier in [lo, hi] for the given axis.
func (p Persona) Bias(axis string, lo, hi float64) float64 {
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo + p.unit(axis)*(hi-lo)
}

// Index returns a deterministic index in [0, n) for the given axis.
func (p Persona) Index(axis string, n int) int {
	if n <= 0 {
		return 0
	}
	return int(p.axisValue(axis) % uint64(n))
}

// Subset returns k distinct deterministic indices in [0, n) for the axis — the
// mailbox's preferred subset (e.g. the greetings it tends to use). Callers then
// pick randomly within this subset so each mailbox has a consistent "voice"
// while individual messages still vary. Order is deterministic.
func (p Persona) Subset(axis string, n, k int) []int {
	if n <= 0 || k <= 0 {
		return nil
	}
	if k >= n {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	seen := make(map[int]struct{}, k)
	out := make([]int, 0, k)
	v := p.axisValue(axis)
	// Linear congruential walk seeded by the axis value; deterministic and
	// well-spread for the small n/k we use here.
	for len(out) < k {
		idx := int(v % uint64(n))
		if _, ok := seen[idx]; !ok {
			seen[idx] = struct{}{}
			out = append(out, idx)
		}
		v = v*6364136223846793005 + 1442695040888963407
	}
	return out
}
