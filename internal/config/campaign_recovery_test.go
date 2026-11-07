package config

import "testing"

func TestCampaignIsolationIsOnUnlessExplicitlyDisabled(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", true}, {"true", true}, {"invalid", true}, {"false", false}, {"0", false}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("CAMPAIGN_SEND_ISOLATION_ENABLED", tc.value)
			if got := CampaignSendIsolationEnabled(); got != tc.want {
				t.Fatalf("activation=%v, want %v", got, tc.want)
			}
		})
	}
}
