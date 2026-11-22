package partner

import (
	"context"
	"encoding/base64"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// MaxMailboxDaysPage bounds one page of GET /usage/mailbox-days.
const MaxMailboxDaysPage = 1000

// statementGrace is how long after a month ends its numbers are final.
const statementGrace = 24 * time.Hour

// ParseMonth reads ?month=YYYY-MM as the month's first instant in UTC; empty is the current month.
func ParseMonth(raw string, now time.Time) (time.Time, *errx.Error) {
	if raw == "" {
		n := now.UTC()
		return time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	m, err := time.Parse("2006-01", raw)
	if err != nil {
		return time.Time{}, invalid("month is YYYY-MM.")
	}
	return m, nil
}

// MailboxDayTotals sums a month.
type MailboxDayTotals struct {
	SendingWarmupDays int `json:"sending_warmup_days"`
	WarmupOnlyDays    int `json:"warmup_only_days"`
}

// MailboxDaysReport is GET /usage/mailbox-days.
type MailboxDaysReport struct {
	Month      string                      `json:"month"`
	Data       []models.PartnerMailboxDays `json:"data"`
	Totals     MailboxDayTotals            `json:"totals"`
	Pagination Pagination                  `json:"pagination"`
}

func daysCursor(d models.PartnerMailboxDays) string {
	return base64.RawURLEncoding.EncodeToString([]byte(d.OrgExternalID + "\x00" + d.MailboxID.String()))
}

func afterDaysCursor(cursor string, d models.PartnerMailboxDays) bool {
	raw, _ := base64.RawURLEncoding.DecodeString(cursor)
	org, id, _ := strings.Cut(string(raw), "\x00")
	if d.OrgExternalID != org {
		return d.OrgExternalID > org
	}
	return d.MailboxID.String() > id
}

func validDaysCursor(cursor string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return false
	}
	_, id, ok := strings.Cut(string(raw), "\x00")
	_, err = uuid.Parse(id)
	return ok && err == nil
}

// monthDays is every mailbox of the caller's mode with an enabled day in month, in a stable order.
func (s *Service) monthDays(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, month time.Time) ([]models.PartnerMailboxDays, error) {
	return s.mb.Repo.PartnerMailboxDays(ctx, partnerID, mode, month, month.AddDate(0, 1, 0), s.now().UTC())
}

// MailboxDays is GET /usage/mailbox-days?month=: enabled mailbox-days per mailbox, for reconciliation.
func (s *Service) MailboxDays(ctx context.Context, c *Caller, rawMonth, cursor string, limit int) (*MailboxDaysReport, *errx.Error) {
	if !s.mailboxesOn() {
		return nil, ErrMailboxesDisabled
	}
	month, xerr := ParseMonth(rawMonth, s.now())
	if xerr != nil {
		return nil, xerr
	}
	if cursor != "" && !validDaysCursor(cursor) {
		return nil, invalid("cursor is not valid.")
	}
	rows, err := s.monthDays(ctx, c.Partner.ID, c.Mode, month)
	if err != nil {
		return nil, internal(err)
	}
	out := &MailboxDaysReport{Month: month.Format("2006-01"), Data: []models.PartnerMailboxDays{}}
	for _, r := range rows {
		out.Totals.SendingWarmupDays += r.SendingWarmupDays
		out.Totals.WarmupOnlyDays += r.WarmupOnlyDays
		if cursor != "" && !afterDaysCursor(cursor, r) {
			continue
		}
		if len(out.Data) == limit {
			out.Pagination.HasMore = true
			next := daysCursor(out.Data[limit-1])
			out.Pagination.NextCursor = &next
			continue
		}
		out.Data = append(out.Data, r)
	}
	return out, nil
}

// UsageLine is one workspace's billable mailbox-days in a month.
type UsageLine struct {
	ExternalID        string `json:"external_id"`
	SendingWarmupDays int    `json:"sending_warmup_days"`
	WarmupOnlyDays    int    `json:"warmup_only_days"`
	AmountMicros      int64  `json:"amount_micros"`
}

// UsageReport is GET /usage?month=: what the partner is billed per workspace.
type UsageReport struct {
	Month       string      `json:"month"`
	Final       bool        `json:"final"`
	Data        []UsageLine `json:"data"`
	TotalMicros int64       `json:"total_micros"`
}

// rates are the wholesale prices per mailbox-day; a test-mode workspace bills nothing.
func rates(p *models.Partner, mode models.PartnerMode) (sendingWarmup, warmupOnly int64) {
	if mode != models.PartnerModeLive {
		return 0, 0
	}
	return p.WholesaleSendingWarmupDay, p.WholesaleWarmupOnlyDay
}

// buildStatement adds a month's days up per workspace at the given rates.
func buildStatement(p *models.Partner, mode models.PartnerMode, month time.Time, rows []models.PartnerMailboxDays) *models.PartnerStatement {
	sw, wo := rates(p, mode)
	st := &models.PartnerStatement{PartnerID: p.ID, Mode: mode, Month: month.Format("2006-01"), SendingWarmupDayMicros: sw, WarmupOnlyDayMicros: wo,
		Lines: []models.PartnerStatementLine{}}
	byOrg := map[string]*models.PartnerStatementLine{}
	for _, r := range rows {
		l, ok := byOrg[r.OrgExternalID]
		if !ok {
			l = &models.PartnerStatementLine{OrgExternalID: r.OrgExternalID}
			byOrg[r.OrgExternalID] = l
		}
		l.SendingWarmupDays += r.SendingWarmupDays
		l.WarmupOnlyDays += r.WarmupOnlyDays
	}
	for _, l := range byOrg {
		l.AmountMicros = int64(l.SendingWarmupDays)*sw + int64(l.WarmupOnlyDays)*wo
		st.SendingWarmupDays += int64(l.SendingWarmupDays)
		st.WarmupOnlyDays += int64(l.WarmupOnlyDays)
		st.AmountMicros += l.AmountMicros
		st.Lines = append(st.Lines, *l)
	}
	sort.Slice(st.Lines, func(i, j int) bool { return st.Lines[i].OrgExternalID < st.Lines[j].OrgExternalID })
	return st
}

// statementFor is a closed month's statement, or the running one at today's rates.
func (s *Service) statementFor(ctx context.Context, p *models.Partner, mode models.PartnerMode, month time.Time) (*models.PartnerStatement, bool, error) {
	if st, err := s.mb.Repo.GetPartnerStatement(ctx, p.ID, mode, month); err != nil || st != nil {
		return st, st != nil, err
	}
	rows, err := s.monthDays(ctx, p.ID, mode, month)
	if err != nil {
		return nil, false, err
	}
	return buildStatement(p, mode, month, rows), false, nil
}

// Usage is GET /usage?month=.
func (s *Service) Usage(ctx context.Context, c *Caller, rawMonth string) (*UsageReport, *errx.Error) {
	if !s.mailboxesOn() {
		return nil, ErrMailboxesDisabled
	}
	month, xerr := ParseMonth(rawMonth, s.now())
	if xerr != nil {
		return nil, xerr
	}
	st, final, err := s.statementFor(ctx, c.Partner, c.Mode, month)
	if err != nil {
		return nil, internal(err)
	}
	out := &UsageReport{Month: st.Month, Final: final, Data: []UsageLine{}, TotalMicros: st.AmountMicros}
	for _, l := range st.Lines {
		out.Data = append(out.Data, UsageLine{ExternalID: l.OrgExternalID, SendingWarmupDays: l.SendingWarmupDays, WarmupOnlyDays: l.WarmupOnlyDays, AmountMicros: l.AmountMicros})
	}
	return out, nil
}

// CloseStatements writes every month that ended at least a day ago and has no statement yet, for live mode.
func (s *Service) CloseStatements(ctx context.Context) error {
	if !s.mailboxesOn() {
		return nil
	}
	partners, err := s.Repo.ListPartners(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	for i := range partners {
		p := &partners[i]
		first, err := s.mb.Repo.FirstPartnerEnablement(ctx, p.ID, models.PartnerModeLive)
		if err != nil || first == nil {
			if err != nil {
				errs.CaptureException(err)
			}
			continue
		}
		f := first.UTC()
		for m := time.Date(f.Year(), f.Month(), 1, 0, 0, 0, 0, time.UTC); !now.Before(m.AddDate(0, 1, 0).Add(statementGrace)); m = m.AddDate(0, 1, 0) {
			if st, err := s.mb.Repo.GetPartnerStatement(ctx, p.ID, models.PartnerModeLive, m); err != nil || st != nil {
				continue
			}
			rows, err := s.monthDays(ctx, p.ID, models.PartnerModeLive, m)
			if err != nil {
				errs.CaptureException(err)
				continue
			}
			if err := s.mb.Repo.InsertPartnerStatement(ctx, buildStatement(p, models.PartnerModeLive, m, rows)); err != nil {
				errs.CaptureException(err)
			}
		}
	}
	return nil
}

// AdminStatement is one month for the admin panel: closed, or running at today's rates.
func (s *Service) AdminStatement(ctx context.Context, partnerID uuid.UUID, rawMonth string) (*models.PartnerStatement, *errx.Error) {
	if !s.mailboxesOn() {
		return nil, ErrMailboxesDisabled
	}
	p, err := s.Repo.GetPartner(ctx, partnerID)
	if err != nil {
		return nil, internal(err)
	}
	if p == nil {
		return nil, errx.ErrNotFound
	}
	month, xerr := ParseMonth(rawMonth, s.now())
	if xerr != nil {
		return nil, xerr
	}
	st, _, err := s.statementFor(ctx, p, models.PartnerModeLive, month)
	if err != nil {
		return nil, internal(err)
	}
	return st, nil
}

// AdminStatements lists a partner's closed months, newest first.
func (s *Service) AdminStatements(ctx context.Context, partnerID uuid.UUID) ([]models.PartnerStatement, *errx.Error) {
	if !s.mailboxesOn() {
		return []models.PartnerStatement{}, nil
	}
	out, err := s.mb.Repo.ListPartnerStatements(ctx, partnerID, models.PartnerModeLive, 36)
	if err != nil {
		return nil, internal(err)
	}
	return out, nil
}

// Balance is GET /organizations/{external_id}/balance: what pays for AI and usage.
type Balance struct {
	Owner                   string        `json:"owner"`
	BalanceMicros           int64         `json:"balance_micros"`
	IncludedMicros          int64         `json:"included_micros"`
	IncludedRemainingMicros int64         `json:"included_remaining_micros"`
	PeriodEnd               time.Time     `json:"period_end"`
	Usage                   []BalanceLine `json:"usage"`
}

// BalanceLine is one kind of usage charged to the balance this period.
type BalanceLine struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Quantity     int64  `json:"quantity"`
	AmountMicros int64  `json:"amount_micros"`
}

// CreditInput is POST /organizations/{external_id}/balance/credits.
type CreditInput struct {
	AmountMicros int64  `json:"amount_micros"`
	Reference    string `json:"reference"`
	Description  string `json:"description"`
}

// Top-up range a partner credit is held to, Warmbly's own.
const (
	minCreditMicros = 5_000_000
	maxCreditMicros = 5_000_000_000
)

// GetBalance reads a managed workspace's balance; the managed plan includes no allowance.
func (s *Service) GetBalance(ctx context.Context, c *Caller, externalID string) (*Balance, *errx.Error) {
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	return s.balance(ctx, *po.OrganizationID)
}

func (s *Service) balance(ctx context.Context, orgID uuid.UUID) (*Balance, *errx.Error) {
	now := s.now().UTC()
	out := &Balance{Owner: "organization", PeriodEnd: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0), Usage: []BalanceLine{}}
	if s.Balances == nil {
		return out, nil
	}
	l, err := s.Balances.GetBalance(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	if l != nil {
		out.BalanceMicros = int64(l.Total())
		out.IncludedRemainingMicros = int64(l.Monthly)
	}
	return out, nil
}

// CreditBalance adds money the partner charged its customer for; a reference already credited applies nothing.
func (s *Service) CreditBalance(ctx context.Context, c *Caller, externalID string, in CreditInput) (*Balance, *errx.Error) {
	if s.mb.Credits == nil {
		return nil, ErrMailboxesDisabled
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	ref := strings.TrimSpace(in.Reference)
	if ref == "" || len(ref) > 255 {
		return nil, invalid("reference is 1-255 characters.")
	}
	if in.AmountMicros < minCreditMicros || in.AmountMicros > maxCreditMicros {
		return nil, errx.NewWithIdentifier(errx.Unprocessable, "invalid_request", "amount_micros is 5,000,000 to 5,000,000,000.")
	}
	reason := "Balance credit from " + c.Partner.DisplayName
	key := "partner:credit:" + c.Partner.ID.String() + ":" + string(c.Mode) + ":" + po.ExternalID + ":" + ref
	if _, err := s.mb.Credits.GrantPurchased(ctx, *po.OrganizationID, models.Micro(in.AmountMicros), reason, key); err != nil {
		return nil, internal(err)
	}
	return s.balance(ctx, *po.OrganizationID)
}
