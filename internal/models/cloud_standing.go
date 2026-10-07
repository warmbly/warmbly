package models

import "time"

func (m CloudLinkMailbox) EffectiveStanding(now time.Time) *WarmupHealthInfo {
	if h := m.Standing; h != nil && (h.State == string(WarmupHealthBlocked) || h.State == string(WarmupHealthQuarantined)) && (h.BlockedUntil == nil || h.BlockedUntil.After(now)) {
		return h
	}
	if m.EnrollmentState != "active" || m.Standing == nil || m.StandingObservedAt == nil || m.StandingObservedAt.After(now) || !m.StandingObservedAt.After(now.Add(-15*time.Minute)) {
		return &WarmupHealthInfo{State: string(WarmupHealthBlocked), Reason: "cloud_evidence_unavailable"}
	}
	return m.Standing
}
