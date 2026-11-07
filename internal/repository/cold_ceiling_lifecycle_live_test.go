package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

func TestLiveColdCeilingFollowsWarmupLifecycleSequence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []string
		lifted  bool
	}{
		{"running", []string{"start"}, false},
		{"paused", []string{"start", "pause"}, false},
		{"stopped", []string{"start", "stop"}, true},
		{"stop then pause", []string{"start", "stop", "pause"}, false},
		{"pause then stop", []string{"start", "pause", "stop"}, true},
		{"stop then start", []string{"start", "stop", "start"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := lineageFixture(t)
			emails := NewEmailRepostory(&db.DB{Pool: f.pool}, nil)
			for _, action := range tc.actions {
				if _, xerr := emails.SetWarmupLifecycle(t.Context(), f.org.String(), f.sender.String(), action); xerr != nil {
					t.Fatal(action, xerr)
				}
			}
			states, err := NewWarmupRepository(f.pool).ColdRampStateForAccounts(t.Context(), []uuid.UUID{f.sender}, time.Now().Add(-14*24*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if lifted := !states[f.sender].CeilingApplies(); lifted != tc.lifted {
				t.Fatalf("ceiling lifted = %v, want %v (%+v)", lifted, tc.lifted, states[f.sender])
			}
		})
	}
}

func TestLiveColdRampStateReturnsStopStampOnlyInStopState(t *testing.T) {
	stamp := "NOW()-INTERVAL '1 hour'"
	for _, tc := range []struct {
		name, set string
		stamped   bool
	}{
		{"off with stamp", "test_mode='off',warmup_stopped_at=" + stamp, true},
		{"diagnostic both directions off", "test_mode='diagnostic',test_send_enabled=false,test_receive_enabled=false,warmup_stopped_at=" + stamp, false},
		{"diagnostic sending on", "test_mode='diagnostic',test_send_enabled=true,test_receive_enabled=false,warmup_stopped_at=" + stamp, false},
		{"paused without stamp", "test_mode='diagnostic',test_send_enabled=false,warmup_paused_at=NOW(),warmup_stopped_at=NULL", false},
		{"warming again with a stale stamp", "test_mode='diagnostic',test_send_enabled=true,test_receive_enabled=true,warmup_stopped_at=NOW()-INTERVAL '30 days'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := lineageFixture(t)
			if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET `+tc.set+` WHERE id=$1`, f.sender); err != nil {
				t.Fatal(err)
			}
			states, err := NewWarmupRepository(f.pool).ColdRampStateForAccounts(t.Context(), []uuid.UUID{f.sender}, time.Now().Add(-14*24*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if got := states[f.sender].WarmupStoppedAt != nil; got != tc.stamped {
				t.Fatalf("stop stamp returned = %v, want %v", got, tc.stamped)
			}
		})
	}
}
