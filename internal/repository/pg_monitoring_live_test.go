package repository

import (
	"testing"
)

func TestMonitoringAggregatesDoNotLinkToCampaignOnlyDetails(t *testing.T) {
	for _, id := range []string{"sends", "dispatch"} {
		if link := monitoringSections[id].investigate; link != "" {
			t.Errorf("%s links to %q, which does not cover its measured tasks", id, link)
		}
	}
}
