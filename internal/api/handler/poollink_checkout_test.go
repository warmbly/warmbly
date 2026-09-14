package handler

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func poolPtr(s string) *string { return &s }

// A period with no price of its own must never borrow the other one's: the
// customer would be charged a month's price for a year, or a year's for a
// month.
func TestPoolPriceForNeverFallsBackAcrossPeriods(t *testing.T) {
	monthlyOnly := &models.Plan{StripePriceID: poolPtr("price_month")}
	if got := poolPriceFor(monthlyOnly, "year"); got != "" {
		t.Fatalf("yearly on a monthly-only plan = %q, want empty", got)
	}
	if got := poolPriceFor(monthlyOnly, "month"); got != "price_month" {
		t.Fatalf("monthly = %q, want price_month", got)
	}

	yearlyOnly := &models.Plan{StripePriceIDYearly: poolPtr("price_year")}
	if got := poolPriceFor(yearlyOnly, "month"); got != "" {
		t.Fatalf("monthly on a yearly-only plan = %q, want empty", got)
	}
	if got := poolPriceFor(yearlyOnly, "year"); got != "price_year" {
		t.Fatalf("yearly = %q, want price_year", got)
	}
}

// A column that was set to whitespace is not a price, and a checkout opened
// with one fails at Stripe rather than here.
func TestPoolPriceForTreatsBlankAsUnset(t *testing.T) {
	blank := &models.Plan{StripePriceID: poolPtr("   "), StripePriceIDYearly: poolPtr("")}
	if got := poolPriceFor(blank, "month"); got != "" {
		t.Fatalf("blank monthly = %q, want empty", got)
	}
	if got := poolPriceFor(blank, "year"); got != "" {
		t.Fatalf("blank yearly = %q, want empty", got)
	}
	if got := poolPriceFor(nil, "month"); got != "" {
		t.Fatalf("nil plan = %q, want empty", got)
	}
}
