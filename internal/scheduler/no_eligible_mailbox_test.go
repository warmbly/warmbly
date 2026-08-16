package scheduler

import (
	"errors"
	"testing"
)

// ErrNoEligibleMailbox has to stay assignable to ErrNoEmailAccounts: three
// callers (the campaign handler, the campaign task and the reconciler) pause a
// campaign on errors.Is(err, ErrNoEmailAccounts), and narrowing the error must
// not silently change that behaviour.
func TestNoEligibleMailboxWrapsNoEmailAccounts(t *testing.T) {
	if !errors.Is(ErrNoEligibleMailbox, ErrNoEmailAccounts) {
		t.Fatal("ErrNoEligibleMailbox no longer matches ErrNoEmailAccounts; existing pause behaviour would regress")
	}
	// The reverse must not hold, or the handler cannot tell them apart.
	if errors.Is(ErrNoEmailAccounts, ErrNoEligibleMailbox) {
		t.Fatal("ErrNoEmailAccounts matches ErrNoEligibleMailbox, so the two cases are indistinguishable")
	}
}
