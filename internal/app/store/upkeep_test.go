package store

import (
	"testing"
	"time"
)

// The balance's day: a month of mailboxes over 30 days, each renewal over the days until it is charged, and AI as it ran.
func TestBurnSpreadsUpkeepOverTheDaysItCovers(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	u := &Upkeep{
		MonthlyMailboxesMicros: 9_000_000,
		Renewals: []UpcomingRenewal{
			{Domain: "getacme.com", Micros: 12_000_000, ChargedOn: now.Add(10 * 24 * time.Hour)},
			// Charged today: the whole price lands on one day, never divided by zero.
			{Domain: "tryacme.com", Micros: 2_000_000, ChargedOn: now},
		},
	}
	b := BurnPerDay(u, 500_000, now)
	if b.MailboxesPerDayMicros != 300_000 || b.RenewalsPerDayMicros != 1_200_000+2_000_000 || b.AIPerDayMicros != 500_000 {
		t.Fatalf("burn parts = %+v", b)
	}
	if b.PerDayMicros != 300_000+3_200_000+500_000 || b.RenewalsDueMicros != 14_000_000 {
		t.Errorf("burn = %+v", b)
	}
	// Nothing bought and no AI: nothing burns, so the balance lasts.
	if z := BurnPerDay(nil, -5, now); z.PerDayMicros != 0 || z.AIPerDayMicros != 0 {
		t.Errorf("empty burn = %+v", z)
	}
}
