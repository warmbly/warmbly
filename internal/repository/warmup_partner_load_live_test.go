package repository

import (
	"testing"

	"github.com/warmbly/warmbly/internal/config"
)

func TestPartnerInboundCapMatchesTheSQLRule(t *testing.T) {
	for _, tc := range []struct{ sent, share, want int }{
		{0, 100, config.WarmupInboundDailyFloor},
		{35, 100, max(config.WarmupInboundDailyFloor, 5*config.WarmupInboundDailyMultiple)},
		{36, 100, max(config.WarmupInboundDailyFloor, 6*config.WarmupInboundDailyMultiple)},
		{10_000, 100, config.WarmupInboundDailyCeiling},
		{0, 75, config.WarmupInboundDailyFloor * 75 / 100},
	} {
		if got := partnerInboundCap(tc.sent, tc.share); got != tc.want {
			t.Fatalf("cap(%d, %d%%) = %d, want %d", tc.sent, tc.share, got, tc.want)
		}
	}
}
