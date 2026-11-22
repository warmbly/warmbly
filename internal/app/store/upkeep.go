package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// upkeepHorizon is how far ahead a domain renewal counts toward what the balance has to cover.
const upkeepHorizon = 60 * 24 * time.Hour

// Upkeep is what the workspace's store purchases take from the balance: the monthly mailboxes and the
// renewals charged within the horizon, each on the day it is charged.
type Upkeep struct {
	MonthlyMailboxesMicros int64
	Renewals               []UpcomingRenewal
	// BillingDay is the day of the month mailboxes are billed; 0 before the first mailbox.
	BillingDay int
}

// UpcomingRenewal is one auto-renewing domain charged within the horizon.
type UpcomingRenewal struct {
	Domain    string
	Micros    int64
	ChargedOn time.Time
}

// Upkeep reads the workspace's store upkeep; the zero value when the store is off or holds nothing.
func (s *Service) Upkeep(ctx context.Context, orgID uuid.UUID) (*Upkeep, error) {
	out := &Upkeep{}
	if !s.Enabled() {
		return out, nil
	}
	out.MonthlyMailboxesMicros = s.monthlyMailboxes(ctx, orgID)
	if acct, err := s.Repo.GetAccount(ctx, orgID); err == nil && acct != nil {
		out.BillingDay = acct.BillDay
	}
	domains, err := s.Repo.ListDomains(ctx, orgID, nil, maxPageLimit*5)
	if err != nil {
		return nil, err
	}
	now := s.now()
	for i := range domains {
		d := &domains[i]
		if !d.AutoRenew || d.ExpiresAt == nil || d.Status != "active" || d.Source == "connected" {
			continue
		}
		// Renewals are charged ahead of expiry, never before today.
		on := d.ExpiresAt.Add(-renewAhead)
		if on.Before(now) {
			on = now
		}
		if on.After(now.Add(upkeepHorizon)) {
			continue
		}
		price := int64(d.RenewRetailMicros)
		if d.LastSeenRenewMicros > 0 {
			price = int64(d.LastSeenRenewMicros)
		}
		out.Renewals = append(out.Renewals, UpcomingRenewal{Domain: d.Name, Micros: price, ChargedOn: on})
	}
	return out, nil
}

// Burn is what the balance spends a day, and from what.
type Burn struct {
	PerDayMicros          int64
	MailboxesPerDayMicros int64
	RenewalsPerDayMicros  int64
	AIPerDayMicros        int64
	RenewalsDueMicros     int64
}

// BurnPerDay spreads the upkeep over the days it covers: a month of mailboxes over 30 days, each
// renewal over the days left until it is charged, plus the average daily AI spend.
func BurnPerDay(u *Upkeep, aiPerDay int64, now time.Time) Burn {
	var b Burn
	if u != nil {
		b.MailboxesPerDayMicros = u.MonthlyMailboxesMicros / 30
		for _, r := range u.Renewals {
			days := int64(r.ChargedOn.Sub(now).Hours() / 24)
			b.RenewalsPerDayMicros += r.Micros / max(1, days)
			b.RenewalsDueMicros += r.Micros
		}
	}
	b.AIPerDayMicros = max(0, aiPerDay)
	b.PerDayMicros = b.MailboxesPerDayMicros + b.RenewalsPerDayMicros + b.AIPerDayMicros
	return b
}
