package credits

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/models"
)

func TestPriceIsProviderCostPlusMarkup(t *testing.T) {
	prices := instancesettings.DefaultUsage()
	// A million tokens each way on gpt-6-luna: $0.10 + $0.50 = $0.60, plus 25%.
	charge, cost := Price(prices, AIUsage("gpt-6-luna", 1_000_000, 1_000_000))
	if cost != 600_000 || charge != 750_000 {
		t.Fatalf("luna = %v charged on %v cost, want $0.75 on $0.60", charge.USD(), cost.USD())
	}
	// A model with no row of its own is priced at the "*" row.
	if charge, _ := Price(prices, AIUsage("some-new-model", 1_000_000, 0)); charge != 3_750_000 {
		t.Fatalf("fallback = %v, want $3.75", charge.USD())
	}
	// A call that used anything is never free.
	if charge, _ := Price(prices, AIUsage("gpt-6-luna", 1, 0)); charge != 1 {
		t.Fatalf("one token = %d micro, want 1", charge)
	}
	if charge, _ := Price(prices, UnitUsage(instancesettings.UnitPlacementTest, 1)); charge != models.MicroPerUSD/2 {
		t.Fatalf("placement test = %v, want $0.50", charge.USD())
	}
	// The lead finder's website read and person email bill $0.02, a search page $0.04.
	for unit, want := range map[string]models.Micro{
		instancesettings.UnitLeadWebsiteRead: 20_000,
		instancesettings.UnitLeadPersonEmail: 20_000,
		instancesettings.UnitLeadSearchPage:  40_000,
	} {
		if charge, _ := Price(prices, UnitUsage(unit, 1)); charge != want {
			t.Fatalf("%s = %d micro, want %d", unit, charge, want)
		}
	}
}

func TestLatestMonthlyAnchor(t *testing.T) {
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		start, now, want string
		ok               bool
	}{
		{"2026-01-15", "2026-02-01", "", false},          // still the first month: the invoice covered it
		{"2026-01-15", "2026-02-15", "2026-02-15", true}, // the first anniversary
		{"2026-01-15", "2026-07-20", "2026-07-15", true}, // mid-year
		{"2026-01-31", "2026-03-01", "2026-02-28", true}, // a short month lands on its last day
		{"2026-01-31", "2026-03-31", "2026-03-31", true}, // and the next month is back on the 31st
		{"2026-03-10", "2027-03-09", "2027-02-10", true}, // the last month before renewal
	}
	for _, c := range cases {
		got, ok := LatestMonthlyAnchor(day(c.start), day(c.now))
		if ok != c.ok || (ok && !got.Equal(day(c.want))) {
			t.Errorf("start %s now %s: got %v %v, want %s %v", c.start, c.now, got.Format("2006-01-02"), ok, c.want, c.ok)
		}
	}
}
