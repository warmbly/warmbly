package repository

import (
	"testing"
)

func TestValidCampaignTransitions(t *testing.T) {
	tests := []struct {
		from    string
		to      string
		allowed bool
	}{
		// Valid transitions
		{"draft", "active", true},
		{"active", "paused", true},
		{"active", "completed", true},
		{"active", "paused_no_accounts", true},
		{"active", "paused_trial_expired", true},
		{"paused", "active", true},
		{"paused", "draft", true},
		{"paused_no_accounts", "active", true},
		{"paused_trial_expired", "active", true},

		// Invalid transitions
		{"completed", "active", false},
		{"completed", "draft", false},
		{"completed", "paused", false},
		{"draft", "completed", false},
		{"draft", "paused", false},
		{"active", "draft", false},
	}

	for _, tc := range tests {
		t.Run(tc.from+"_to_"+tc.to, func(t *testing.T) {
			allowed, ok := validCampaignTransitions[tc.from]
			result := ok && allowed[tc.to]
			if result != tc.allowed {
				t.Errorf("transition %s -> %s: expected allowed=%v, got %v", tc.from, tc.to, tc.allowed, result)
			}
		})
	}
}

func TestCompletedIsTerminal(t *testing.T) {
	allowed := validCampaignTransitions["completed"]
	if len(allowed) != 0 {
		t.Errorf("completed should be terminal state with no valid transitions, got %v", allowed)
	}
}
