package models

// EmailRiskBand classifies a mailbox by reputation risk. The rebalancer
// derives this from WarmupHealthState and writes it into
// email_accounts.risk_band; nothing else should set it. Workers pick up
// mailboxes whose band matches their risk_pool.
type EmailRiskBand string

const (
	EmailRiskBandClean      EmailRiskBand = "clean"
	EmailRiskBandRisky      EmailRiskBand = "risky"
	EmailRiskBandQuarantine EmailRiskBand = "quarantine"
)

// RiskBandFromHealth maps the warmup health state machine into the simpler
// three-bucket risk_band that workers cluster by. The mapping is one-way
// (collapses watch/throttled/quarantined into the recovery pool) — the
// reverse direction is meaningless.
//
//	healthy           → clean
//	watch, throttled  → risky      (degraded but still sending)
//	quarantined,      → quarantine (sending stopped or close to it)
//	blocked
//
// Any state not covered (e.g. a row with NULL warmup state) defaults to
// clean — assume innocent until proven otherwise.
func RiskBandFromHealth(s WarmupHealthState) EmailRiskBand {
	switch s {
	case WarmupHealthWatch, WarmupHealthThrottled:
		return EmailRiskBandRisky
	case WarmupHealthQuarantined, WarmupHealthBlocked:
		return EmailRiskBandQuarantine
	default:
		return EmailRiskBandClean
	}
}

// MatchingRiskPool returns the worker risk_pool that should host mailboxes
// of this band. The naming intentionally mirrors so a band of X always
// goes to a pool of X — keeps the rebalancer trivial.
func (b EmailRiskBand) MatchingRiskPool() WorkerRiskPool {
	switch b {
	case EmailRiskBandRisky:
		return WorkerRiskPoolRisky
	case EmailRiskBandQuarantine:
		return WorkerRiskPoolQuarantine
	default:
		return WorkerRiskPoolClean
	}
}
