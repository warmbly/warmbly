package store

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
)

func retailWith(pct int, fixed int64) instancesettings.StoreRetail {
	r := instancesettings.DefaultStoreRetail()
	r.DomainMarkupPercent, r.DomainMarkupFixedMicros = &pct, &fixed
	return r
}

func TestRetailDomainPrice(t *testing.T) {
	cases := []struct {
		name      string
		pct       int
		fixed     int64
		wholesale int64
		want      int64
	}{
		{"at cost", 0, 0, 11_170_000, 11_170_000},
		{"at cost rounds a fraction of a cent up", 0, 0, 11_170_001, 11_180_000},
		{"ten percent rounds up to the cent", 10, 0, 11_170_000, 12_290_000},
		{"fixed markup", 0, 1_000_000, 11_170_000, 12_170_000},
		{"both", 20, 500_000, 10_000_000, 12_500_000},
		{"nothing to price", 50, 1_000_000, 0, 0},
	}
	for _, tc := range cases {
		if got := retailDomain(retailWith(tc.pct, tc.fixed), tc.wholesale); got != tc.want {
			t.Errorf("%s: retailDomain(%d) = %d, want %d", tc.name, tc.wholesale, got, tc.want)
		}
	}
	if !atCost(retailWith(0, 0)) || atCost(retailWith(0, 1)) {
		t.Error("atCost")
	}
}

func TestRetailSettingsClamp(t *testing.T) {
	neg, huge := int64(-5), 9_000
	r := instancesettings.StoreRetail{MailboxMonthlyMicros: &neg, DomainMarkupPercent: &huge}
	r.Normalize()
	if r.MailboxMonthly() != 0 || r.MarkupPercent() != instancesettings.StoreDomainMarkupMax || r.MarkupFixed() != 0 {
		t.Fatalf("normalized = %d %d %d", r.MailboxMonthly(), r.MarkupPercent(), r.MarkupFixed())
	}
	var empty instancesettings.StoreRetail
	empty.Normalize()
	if empty.MailboxMonthly() != 3_000_000 {
		t.Fatalf("default monthly = %d", empty.MailboxMonthly())
	}
}

func TestPaymentPlan(t *testing.T) {
	cases := []struct {
		name                string
		due, balance        int64
		card                bool
		fromBalance, onCard int64
		needsCard           bool
	}{
		{"balance covers it", 14_170_000, 20_000_000, false, 14_170_000, 0, false},
		{"shortfall on the card", 14_170_000, 5_000_000, true, 5_000_000, 9_170_000, false},
		{"shortfall rounds up to the cent", 10_000_001, 0, true, 0, 10_010_000, false},
		{"tiny shortfall meets the card minimum", 10_000_000, 9_990_000, true, 9_990_000, 500_000, false},
		{"no card", 14_170_000, 0, false, 0, 14_170_000, true},
		{"negative balance counts as none", 1_000_000, -50, true, 0, 1_000_000, false},
	}
	for _, tc := range cases {
		p := plan(tc.due, tc.balance, tc.card, 0)
		if p.FromBalanceMicros != tc.fromBalance || p.CardChargeMicros != tc.onCard || p.NeedsPaymentMethod != tc.needsCard {
			t.Errorf("%s: plan = %+v", tc.name, p)
		}
	}
}

func TestPaymentPlanTopsUpByTheMinimum(t *testing.T) {
	// A $3 shortfall puts $10 on the card: $3 is used now and $7 stays on the balance.
	p := plan(3_000_000, 0, true, 10_000_000)
	if p.CardChargeMicros != 10_000_000 || p.BalanceAfterMicros != 7_000_000 {
		t.Fatalf("plan = %+v", p)
	}
	// The minimum never applies when the balance covers it: no card charge at all.
	if p := plan(3_000_000, 5_000_000, true, 10_000_000); p.CardChargeMicros != 0 || p.BalanceAfterMicros != 2_000_000 {
		t.Fatalf("covered plan = %+v", p)
	}
	// A larger shortfall is charged as it is.
	if p := plan(14_170_000, 2_000_000, true, 10_000_000); p.CardChargeMicros != 12_170_000 || p.BalanceAfterMicros != 0 {
		t.Fatalf("large plan = %+v", p)
	}
}

func TestAddMonthClamps(t *testing.T) {
	cases := map[string]string{
		"2026-01-31": "2026-02-28",
		"2028-01-31": "2028-02-29",
		"2026-03-15": "2026-04-15",
		"2026-12-31": "2027-01-31",
	}
	for in, want := range cases {
		d, _ := time.Parse("2006-01-02", in)
		if got := addMonth(d).Format("2006-01-02"); got != want {
			t.Errorf("addMonth(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestRegistrantValidation(t *testing.T) {
	r, err := testRegistrant().Validate()
	if err != nil || r.Country != "US" {
		t.Fatalf("valid registrant: %+v %v", r, err)
	}
	bad := testRegistrant()
	bad.Phone, bad.Country, bad.FirstName = "415 555", "USA", "visit acme.com"
	_, err = bad.Validate()
	ce, ok := err.(*ContactError)
	if !ok || len(ce.Fields) != 3 {
		t.Fatalf("err = %v, want three fields", err)
	}
}
