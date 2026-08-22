package scheduler

import (
	"errors"
	"testing"
)

// ErrDomainAuthFailing has to stay assignable to ErrNoEmailAccounts for the
// same reason ErrNoEligibleMailbox does: three callers (the campaign handler,
// the campaign task and the reconciler) pause a campaign on
// errors.Is(err, ErrNoEmailAccounts), and narrowing the error must not silently
// change that behaviour.
func TestDomainAuthFailingWrapsNoEmailAccounts(t *testing.T) {
	if !errors.Is(ErrDomainAuthFailing, ErrNoEmailAccounts) {
		t.Fatal("ErrDomainAuthFailing no longer matches ErrNoEmailAccounts; existing pause behaviour would regress")
	}
	if errors.Is(ErrNoEmailAccounts, ErrDomainAuthFailing) {
		t.Fatal("ErrNoEmailAccounts matches ErrDomainAuthFailing, so the two cases are indistinguishable")
	}
}

// The two narrow sentinels must not match each other, or a caller testing them
// in sequence would report a DNS problem as a scheduling one (or the reverse),
// which is the exact mislabelling both were introduced to prevent.
func TestDomainAuthFailingDistinctFromNoEligibleMailbox(t *testing.T) {
	if errors.Is(ErrDomainAuthFailing, ErrNoEligibleMailbox) {
		t.Fatal("ErrDomainAuthFailing matches ErrNoEligibleMailbox; an auth failure would be reported as a sending-window problem")
	}
	if errors.Is(ErrNoEligibleMailbox, ErrDomainAuthFailing) {
		t.Fatal("ErrNoEligibleMailbox matches ErrDomainAuthFailing; a window problem would be reported as a DNS one")
	}
}

// A scheduler with no policy wired must never gate: the persisted auth state
// stays observe-only, which is the safe direction for any deployment that has
// not turned the gate on.
func TestDomainAuthGateNilPolicyNeverEnforces(t *testing.T) {
	s := &schedulerService{}
	enforce, grace := s.domainAuthGate(t.Context())
	if enforce {
		t.Error("domainAuthGate() enforced with no policy wired")
	}
	if grace != 0 {
		t.Errorf("domainAuthGate() grace = %v, want 0", grace)
	}
}
