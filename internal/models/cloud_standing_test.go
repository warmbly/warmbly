package models

import (
	"testing"
	"time"
)

func TestCloudStandingRejectsFuturePositiveWithoutClearingNegative(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Minute)
	m := CloudLinkMailbox{EnrollmentState: "active", StandingObservedAt: &future, Standing: &WarmupHealthInfo{State: string(WarmupHealthHealthy)}}
	if got := m.EffectiveStanding(now); got.State != string(WarmupHealthBlocked) || got.Reason != "cloud_evidence_unavailable" || m.Standing.State != string(WarmupHealthHealthy) {
		t.Fatalf("future positive trusted or stored evidence rewritten: %+v", got)
	}
	m.Standing = &WarmupHealthInfo{State: string(WarmupHealthQuarantined), Reason: "observed_refusal"}
	if got := m.EffectiveStanding(now); got != m.Standing {
		t.Fatal("negative evidence discarded")
	}
}
