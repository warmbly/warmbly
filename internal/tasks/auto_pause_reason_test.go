package tasks

import (
	"fmt"
	"testing"

	"github.com/warmbly/warmbly/internal/scheduler"
)

// Both narrow sentinels wrap ErrNoEmailAccounts, so autoPauseReason has to test
// them before the generic case. Getting the order wrong sends every paused
// campaign the same "no active email accounts" line, which is how a DNS problem
// becomes an afternoon of checking tag configuration.
func TestAutoPauseReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "domain auth",
			err:  scheduler.ErrDomainAuthFailing,
			want: "Campaign auto-paused: every mailbox is sending from a domain that fails SPF or DMARC authentication",
		},
		{
			name: "no eligible mailbox",
			err:  scheduler.ErrNoEligibleMailbox,
			want: "Campaign auto-paused: every mailbox is outside its sending window or over its daily budget",
		},
		{
			name: "generic no accounts",
			err:  scheduler.ErrNoEmailAccounts,
			want: "Campaign auto-paused: no active email accounts available",
		},
		{
			name: "wrapped domain auth still resolves",
			err:  fmt.Errorf("scheduling campaign: %w", scheduler.ErrDomainAuthFailing),
			want: "Campaign auto-paused: every mailbox is sending from a domain that fails SPF or DMARC authentication",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoPauseReason(tt.err); got != tt.want {
				t.Errorf("autoPauseReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The warmup gate must be inert until a policy is wired, so a deployment that
// never turns it on cannot have a warmup send stopped by it.
func TestDomainAuthBlockedNilPolicy(t *testing.T) {
	s := &tasksService{}
	if s.domainAuthBlocked(t.Context(), nil) {
		t.Error("domainAuthBlocked() = true with no policy and no account")
	}
}
