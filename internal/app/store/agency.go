package store

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
)

// AgencyClient is one client workspace the agency caller may see the store of.
type AgencyClient struct {
	ID        uuid.UUID
	Name      string
	AvatarURL *string
}

// Attention is one kind of problem in a client's purchases and how many.
type Attention struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Attention kinds.
const (
	AttentionConnectFailed = "connect_failed"
	AttentionBillingFailed = "billing_failed"
	AttentionRenewalFailed = "renewal_failed"
	AttentionAutoRenewOff  = "auto_renew_off"
	AttentionOrderFailed   = "order_failed"
)

// NextRenewal is a client's soonest domain renewal.
type NextRenewal struct {
	DomainID  uuid.UUID  `json:"domain_id"`
	Domain    string     `json:"domain"`
	ExpiresAt *time.Time `json:"expires_at"`
	AutoRenew bool       `json:"auto_renew"`
}

// AgencyClientStore is one client's purchased infrastructure.
type AgencyClientStore struct {
	OrganizationID     uuid.UUID    `json:"organization_id"`
	Name               string       `json:"name"`
	AvatarURL          *string      `json:"avatar_url"`
	Domains            int          `json:"domains"`
	MailboxesLive      int          `json:"mailboxes_live"`
	MailboxesConnected int          `json:"mailboxes_connected"`
	MonthlySpendMicros int64        `json:"monthly_spend_micros"`
	Renewals30d        int          `json:"renewals_30d"`
	NextRenewal        *NextRenewal `json:"next_renewal"`
	OrdersOpen         int          `json:"orders_open"`
	Attention          []Attention  `json:"attention"`
}

// AgencyRenewal is one upcoming domain renewal across the agency's clients.
type AgencyRenewal struct {
	OrganizationID   uuid.UUID  `json:"organization_id"`
	OrganizationName string     `json:"organization_name"`
	DomainID         uuid.UUID  `json:"domain_id"`
	Domain           string     `json:"domain"`
	ExpiresAt        *time.Time `json:"expires_at"`
	AutoRenew        bool       `json:"auto_renew"`
	RenewMicros      int64      `json:"renew_micros"`
}

// AgencyTotals adds the clients up.
type AgencyTotals struct {
	Domains            int   `json:"domains"`
	MailboxesLive      int   `json:"mailboxes_live"`
	MonthlySpendMicros int64 `json:"monthly_spend_micros"`
	Renewals30d        int   `json:"renewals_30d"`
	Attention          int   `json:"attention"`
}

// AgencyOverview is GET /agency/:agencyId/store.
type AgencyOverview struct {
	Enabled  bool                `json:"enabled"`
	Clients  []AgencyClientStore `json:"clients"`
	Renewals []AgencyRenewal     `json:"renewals"`
	Totals   AgencyTotals        `json:"totals"`
}

// renewalHorizon is how far ahead an agency's overview lists renewals.
const renewalHorizon = 30 * 24 * time.Hour

// AgencyOverview is the purchased infrastructure of the clients the caller
// may see, problems first. A workspace a partner manages keeps the store off,
// so it is left out, and an agency whose every client is managed has no store.
func (s *Service) AgencyOverview(ctx context.Context, clients []AgencyClient) (*AgencyOverview, error) {
	out := &AgencyOverview{Clients: []AgencyClientStore{}, Renewals: []AgencyRenewal{}}
	if !s.Enabled() {
		return out, nil
	}
	byID := map[uuid.UUID]AgencyClient{}
	ids := make([]uuid.UUID, 0, len(clients))
	for _, c := range clients {
		if s.managed(ctx, c.ID) {
			continue
		}
		byID[c.ID] = c
		ids = append(ids, c.ID)
	}
	out.Enabled = len(ids) > 0 || len(clients) == 0
	if len(ids) == 0 {
		return out, nil
	}
	a, err := s.adminStore()
	if err != nil {
		return nil, err
	}
	now := s.now()
	sums, err := a.OrgSummaries(ctx, ids, now)
	if err != nil {
		return nil, err
	}
	renewals, err := a.RenewalsBefore(ctx, ids, now.Add(renewalHorizon), 200)
	if err != nil {
		return nil, err
	}
	next := map[uuid.UUID]*NextRenewal{}
	for _, d := range renewals {
		if _, ok := next[d.OrganizationID]; !ok {
			next[d.OrganizationID] = &NextRenewal{DomainID: d.ID, Domain: d.Name, ExpiresAt: d.ExpiresAt, AutoRenew: d.AutoRenew}
		}
		if len(out.Renewals) < 50 {
			out.Renewals = append(out.Renewals, AgencyRenewal{OrganizationID: d.OrganizationID, OrganizationName: byID[d.OrganizationID].Name,
				DomainID: d.ID, Domain: d.Name, ExpiresAt: d.ExpiresAt, AutoRenew: d.AutoRenew, RenewMicros: int64(d.RenewRetailMicros)})
		}
	}
	for _, sum := range sums {
		c := byID[sum.OrganizationID]
		row := AgencyClientStore{OrganizationID: c.ID, Name: c.Name, AvatarURL: c.AvatarURL, Domains: sum.Domains,
			MailboxesLive: sum.MailboxesLive, MailboxesConnected: sum.MailboxesConnected, MonthlySpendMicros: int64(sum.MonthlyMicros),
			Renewals30d: sum.Renewals30d, NextRenewal: next[c.ID], OrdersOpen: sum.OrdersOpen, Attention: []Attention{}}
		for _, at := range []Attention{
			{AttentionConnectFailed, sum.ConnectFailed}, {AttentionBillingFailed, sum.BillingFailed},
			{AttentionRenewalFailed, sum.RenewalFailed}, {AttentionOrderFailed, sum.OrdersFailed}, {AttentionAutoRenewOff, sum.AutoRenewOff},
		} {
			if at.Count > 0 {
				row.Attention = append(row.Attention, at)
				if at.Kind != AttentionAutoRenewOff {
					out.Totals.Attention += at.Count
				}
			}
		}
		out.Totals.Domains += row.Domains
		out.Totals.MailboxesLive += row.MailboxesLive
		out.Totals.MonthlySpendMicros += row.MonthlySpendMicros
		out.Totals.Renewals30d += row.Renewals30d
		out.Clients = append(out.Clients, row)
	}
	// Problems first, then the biggest spend, then by name.
	sort.SliceStable(out.Clients, func(i, j int) bool {
		pi, pj := problems(out.Clients[i]), problems(out.Clients[j])
		if pi != pj {
			return pi > pj
		}
		if out.Clients[i].MonthlySpendMicros != out.Clients[j].MonthlySpendMicros {
			return out.Clients[i].MonthlySpendMicros > out.Clients[j].MonthlySpendMicros
		}
		return out.Clients[i].Name < out.Clients[j].Name
	})
	return out, nil
}

func problems(c AgencyClientStore) int {
	n := 0
	for _, a := range c.Attention {
		if a.Kind != AttentionAutoRenewOff {
			n += a.Count
		}
	}
	return n
}
