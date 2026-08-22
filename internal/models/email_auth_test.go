package models

import (
	"testing"
	"time"
)

func TestDomainAuthBlocked(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	grace := 72 * time.Hour

	at := func(d time.Duration) *time.Time {
		v := now.Add(d)
		return &v
	}

	tests := []struct {
		name  string
		email Email
		want  bool
	}{
		{
			// The whole point of the "unknown" state: never checked, or DNS
			// could not answer. It must never stop a send.
			name:  "unknown never blocks",
			email: Email{AuthState: AuthStateUnknown, AuthFailingSince: at(-30 * 24 * time.Hour)},
			want:  false,
		},
		{
			name:  "empty state never blocks",
			email: Email{AuthState: "", AuthFailingSince: at(-30 * 24 * time.Hour)},
			want:  false,
		},
		{
			name:  "passing never blocks",
			email: Email{AuthState: AuthStatePassing},
			want:  false,
		},
		{
			// A loader that forgot to select auth_failing_since, or a row the
			// sweep has not stamped yet, must fail open rather than block on a
			// clock it cannot read.
			name:  "failing with no start time never blocks",
			email: Email{AuthState: AuthStateFailing},
			want:  false,
		},
		{
			name:  "failing inside the grace window does not block",
			email: Email{AuthState: AuthStateFailing, AuthFailingSince: at(-1 * time.Hour)},
			want:  false,
		},
		{
			name:  "failing one second before the grace window elapses does not block",
			email: Email{AuthState: AuthStateFailing, AuthFailingSince: at(-grace + time.Second)},
			want:  false,
		},
		{
			name:  "failing exactly at the grace boundary blocks",
			email: Email{AuthState: AuthStateFailing, AuthFailingSince: at(-grace)},
			want:  true,
		},
		{
			name:  "failing past the grace window blocks",
			email: Email{AuthState: AuthStateFailing, AuthFailingSince: at(-30 * 24 * time.Hour)},
			want:  true,
		},
		{
			// A clock skew that puts the start in the future must not block.
			name:  "failing since the future does not block",
			email: Email{AuthState: AuthStateFailing, AuthFailingSince: at(time.Hour)},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.email.DomainAuthBlocked(now, grace); got != tt.want {
				t.Errorf("DomainAuthBlocked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDomainAuthBlockedZeroGrace(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	since := now.Add(-time.Minute)
	e := Email{AuthState: AuthStateFailing, AuthFailingSince: &since}

	// A zero grace is only reachable when a caller resolves the policy as "not
	// enforced" and passes 0 anyway, so it must still be an honest answer
	// rather than a special case: the state has been failing, so it blocks.
	if !e.DomainAuthBlocked(now, 0) {
		t.Error("DomainAuthBlocked() = false with zero grace, want true")
	}
}
