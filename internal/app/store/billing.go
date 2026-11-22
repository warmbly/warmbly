package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

const (
	// renewAhead is how long before expiry an auto-renewing domain is charged and renewed.
	renewAhead = 30 * 24 * time.Hour
	// renewNoticeLead is how long the upcoming-renewal notice comes before that charge.
	renewNoticeLead = 7 * 24 * time.Hour
	// renewUrgent renews without waiting on the notice, so a domain is never lost to it.
	renewUrgent = 7 * 24 * time.Hour
	retryEvery  = 24 * time.Hour
)

const (
	domainsLink   = "/app/emails/domains?tab=purchased"
	mailboxesLink = "/app/emails"
	balanceLink   = "/app/settings/billing/balance"
	// payLink opens the billing page's card setup; saving a card charges what is unpaid at once.
	payLink   = "/app/settings/billing/balance?update_card=1"
	plansLink = "/app/settings/billing/plans"
)

// Bill is one billing pass.
func (s *Service) Bill(ctx context.Context) error {
	return errors.Join(s.noticeDomains(ctx), s.renewDue(ctx), s.refreshPeriods(ctx), s.billMailboxes(ctx), s.noticePlans(ctx), s.housekeep(ctx),
		s.PushDelegation(ctx), s.syncIdentities(ctx))
}

// syncIdentities brings every workspace's account identities up to this instance's configuration.
func (s *Service) syncIdentities(ctx context.Context) error {
	orgs, err := s.Repo.AccountOrgs(ctx, 10_000)
	if err != nil {
		return err
	}
	for _, orgID := range orgs {
		acct, err := s.Repo.GetAccount(ctx, orgID)
		if err != nil || acct == nil || acct.GraphKitAccountID == "" {
			continue
		}
		if err := s.syncIdentity(ctx, acct); err != nil {
			log.Info().Str("org_id", orgID.String()).Str("reason", reasonOf(err)).Msg("store: account identities not set yet")
		}
	}
	return nil
}

// noticePlans tells a workspace once, when its plan ends, what that means for
// the mailboxes it bought: they stay and keep billing, while campaign sending
// and warmup on them stop until a plan is active again.
func (s *Service) noticePlans(ctx context.Context) error {
	orgs, err := s.Repo.OrgsWithLiveMailboxes(ctx, 10_000)
	if err != nil {
		return err
	}
	for _, orgID := range orgs {
		if s.managed(ctx, orgID) {
			continue
		}
		active, err := s.planActive(ctx, orgID)
		if err != nil {
			continue
		}
		if active {
			_, _ = s.Repo.SetPlanNotice(ctx, orgID, nil)
			continue
		}
		now := s.now()
		won, err := s.Repo.SetPlanNotice(ctx, orgID, &now)
		if err != nil || !won {
			continue
		}
		s.notify(ctx, orgID, "Sending and warmup paused on your purchased mailboxes",
			fmt.Sprintf("Your plan ended, so campaign sending and warmup stopped on the mailboxes you bought. They keep working for reading and replying, and keep billing at %s a month each while you have them. Choose a plan to turn sending and warmup back on.",
				usd(s.retail(ctx).MailboxMonthly())),
			plansLink, "store_plan_ended")
	}
	return nil
}

func (s *Service) housekeep(ctx context.Context) error {
	now := s.now()
	if _, err := s.Repo.PruneQuotes(ctx, now.Add(-24*time.Hour)); err != nil {
		return err
	}
	_, err := s.Repo.PruneWebhookEvents(ctx, now.Add(-31*24*time.Hour))
	return err
}

// ---- domain renewals ----------------------------------------------------

func (s *Service) renewDue(ctx context.Context) error {
	now := s.now()
	due, err := s.Repo.DueDomainRenewals(ctx, now.Add(renewAhead), now.Add(-retryEvery), now.Add(-renewNoticeLead), now.Add(renewUrgent), 200)
	if err != nil {
		return err
	}
	for i := range due {
		d := &due[i]
		if _, err := s.renew(ctx, d, false); err != nil {
			log.Info().Str("store_domain_id", d.ID.String()).Str("reason", reasonOf(err)).Msg("store: domain renewal did not go through")
		}
	}
	return nil
}

func reasonOf(err error) string {
	var xerr *errx.Error
	if errors.As(err, &xerr) {
		return xerr.ResponseCode()
	}
	return err.Error()
}

// RenewNow renews a domain for one more year, charged at its current price.
func (s *Service) RenewNow(ctx context.Context, orgID, id uuid.UUID) (*Domain, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return s.renew(ctx, d, true)
}

// renewKey keys one renewal attempt: the expiry it renews from, and the
// attempts already refunded, so a retry replays the same charge and a refused
// attempt is never confused with the next one.
func renewKey(d *models.StoreDomain) string {
	return renewPrefix(d) + strconv.Itoa(d.RenewalFailures)
}

// renewPrefix is the part of a renewal key naming the expiry it renews from.
func renewPrefix(d *models.StoreDomain) string {
	return "store:renew:" + d.ID.String() + ":" + strconv.FormatInt(d.ExpiresAt.Unix(), 10) + ":"
}

// renew charges the current renewal price and renews from the stored expiry.
// GraphKit renews only when expected_expires_at equals the expiry, so a retry
// either replays or reports the renewal that already happened, which is adopted.
// A charge already on the ledger is never collected again: the retry goes
// straight to GraphKit under the same key, whatever auto-renew says now.
func (s *Service) renew(ctx context.Context, d *models.StoreDomain, manual bool) (*Domain, error) {
	orgID := d.OrganizationID
	if d.Source == models.StoreDomainConnected {
		return nil, registeredElsewhere()
	}
	notRenewable := errx.NewWithIdentifier(errx.Conflict, CodeDomainNotRenewable, "This domain can't be renewed.")
	if d.ExpiresAt == nil {
		return nil, notRenewable
	}
	key := renewKey(d)
	if d.RenewalChargeKey != nil {
		if !strings.HasPrefix(*d.RenewalChargeKey, renewPrefix(d)) {
			// The expiry moved since that charge: the renewal it paid for happened.
			if err := s.Repo.SetRenewalCharge(ctx, orgID, d.ID, nil); err != nil {
				return nil, err
			}
			d.RenewalChargeKey = nil
			if !manual {
				return nil, nil
			}
		} else {
			// A charge in flight is settled under its own key.
			key = *d.RenewalChargeKey
		}
	}
	if d.Status != graphkit.DomainActive && d.Status != graphkit.DomainExpired {
		if d.RenewalChargeKey != nil {
			s.abandonRenewal(ctx, d, key)
		}
		return nil, notRenewable
	}
	live, gerr := s.api().GetDomain(ctx, d.GraphKitDomainID)
	if gerr != nil {
		return nil, s.upstream(ctx, "domain.renew.read", gerr)
	}
	price := retailDomain(s.retail(ctx), live.RenewMicros)
	reason := "Domain renewal · " + d.Name + " · 1 year"
	if s.metered() && price > 0 {
		prior, err := s.Credits.ChargeOf(ctx, orgID, key)
		if err != nil {
			return nil, err
		}
		if prior != nil && prior.Refunded > 0 {
			// Refunded before its failure was recorded: this attempt is over, the next one has its own key.
			if err := s.Repo.RecordDomainRenewalFailure(context.WithoutCancel(ctx), orgID, d.ID); err != nil {
				return nil, err
			}
			return nil, errx.NewWithIdentifier(errx.Conflict, CodeStoreStateConflict, "This renewal was just refunded. Try again.")
		}
		if prior == nil {
			if !manual && !d.AutoRenew {
				// Listed as in flight but never charged, and nobody asked for it since.
				return nil, s.Repo.SetRenewalCharge(ctx, orgID, d.ID, nil)
			}
			if err := s.chargeRenewal(ctx, d, price, key, reason, manual); err != nil {
				return nil, err
			}
		}
	}
	got, gerr := s.api().RenewDomain(ctx, d.GraphKitDomainID, 1, *d.ExpiresAt, key)
	if gerr != nil {
		ge, _ := graphkit.AsError(gerr)
		switch {
		case ge != nil && ge.Code == graphkit.CodeExpectedExpiryMismatch:
			cur := ge.CurrentDomain()
			if cur == nil {
				cur, _ = s.api().GetDomain(ctx, d.GraphKitDomainID)
			}
			if cur != nil && cur.ExpiresAt != nil && cur.ExpiresAt.After(*d.ExpiresAt) {
				// The renewal this charge paid for already happened.
				got = cur
				break
			}
			s.refundRenewal(ctx, d, key, reason)
			if cur != nil {
				_, _ = s.upsertDomain(ctx, orgID, nil, cur)
			}
			return nil, s.upstream(ctx, "domain.renew", gerr)
		case ge != nil && ge.Code == graphkit.CodeRenewalInProgress, !graphkit.Definitive(gerr):
			// Undecided: the charge stands and the next pass retries under the same key.
			return nil, s.upstream(ctx, "domain.renew", gerr)
		default:
			s.refundRenewal(ctx, d, key, reason)
			if !manual {
				s.notify(ctx, orgID, d.Name+" could not be renewed",
					"We could not renew "+d.Name+", and nothing was charged. Renew it from the domain's page before "+d.ExpiresAt.Format("Jan 2, 2006")+".",
					domainsLink, "store_renew_failed_"+d.ID.String())
			}
			return nil, s.upstream(ctx, "domain.renew", gerr)
		}
	}
	if got.ExpiresAt != nil {
		if err := s.Repo.RecordDomainRenewal(context.WithoutCancel(ctx), orgID, d.ID, *got.ExpiresAt); err != nil {
			return nil, err
		}
	}
	row, err := s.upsertDomain(ctx, orgID, nil, got)
	if err != nil || row == nil {
		return nil, err
	}
	out := domainView(row)
	return &out, nil
}

// chargeRenewal collects and debits one renewal attempt, marking it in flight
// before the debit. A refused payment ends the attempt, so the next one, after
// the card changed, is charged under a new key rather than replaying the refusal.
func (s *Service) chargeRenewal(ctx context.Context, d *models.StoreDomain, price int64, key, reason string, manual bool) error {
	orgID := d.OrganizationID
	refused := func(cause error) error {
		if manual {
			_ = s.Repo.RecordDomainRenewalFailure(context.WithoutCancel(ctx), orgID, d.ID)
			return cause
		}
		return s.renewalUnpaid(ctx, d, price, cause)
	}
	if err := s.collect(ctx, orgID, price, key, reason, !manual); err != nil {
		if paymentFailed(err) {
			return refused(err)
		}
		return err
	}
	if err := s.Repo.SetRenewalCharge(ctx, orgID, d.ID, &key); err != nil {
		return err
	}
	if _, err := s.Credits.ChargeAmount(ctx, orgID, micro(price), reason, key); err != nil {
		if !errors.Is(err, credits.ErrInsufficientCredits) {
			return err
		}
		if manual {
			_ = refused(err)
			return errx.NewWithIdentifier(errx.PaymentRequired, CodeInsufficientBalance, "Your balance can't cover this renewal. Add money and try again.")
		}
		return refused(err)
	}
	return nil
}

// abandonRenewal settles a charged renewal on a domain that can no longer be
// renewed: adopted when GraphKit renewed it, refunded otherwise.
func (s *Service) abandonRenewal(ctx context.Context, d *models.StoreDomain, key string) {
	live, gerr := s.api().GetDomain(ctx, d.GraphKitDomainID)
	if gerr != nil {
		logUpstream("domain.renew.read", gerr)
		return
	}
	if live.ExpiresAt != nil && live.ExpiresAt.After(*d.ExpiresAt) {
		if err := s.Repo.RecordDomainRenewal(context.WithoutCancel(ctx), d.OrganizationID, d.ID, *live.ExpiresAt); err == nil {
			_, _ = s.upsertDomain(ctx, d.OrganizationID, nil, live)
		}
		return
	}
	s.refundRenewal(ctx, d, key, "Domain renewal · "+d.Name+" · 1 year")
}

// refundRenewal gives back an attempt's charge and moves on to the next attempt's key.
// A refund that does not go through leaves the attempt in flight, so the next pass tries again.
func (s *Service) refundRenewal(ctx context.Context, d *models.StoreDomain, key, reason string) {
	ctx = context.WithoutCancel(ctx)
	if s.metered() {
		if _, err := s.Credits.RefundCharge(ctx, d.OrganizationID, key, "Refund · "+reason); err != nil {
			log.Error().Err(err).Str("store_domain_id", d.ID.String()).Msg("store: refunding a renewal failed; the billing pass retries")
			return
		}
	}
	_ = s.Repo.RecordDomainRenewalFailure(ctx, d.OrganizationID, d.ID)
}

func (s *Service) renewalUnpaid(ctx context.Context, d *models.StoreDomain, price int64, cause error) error {
	_ = s.Repo.RecordDomainRenewalFailure(context.WithoutCancel(ctx), d.OrganizationID, d.ID)
	s.notify(ctx, d.OrganizationID, d.Name+" renewal payment failed",
		fmt.Sprintf("We could not charge %s to renew %s for another year: %s We try again tomorrow. It expires on %s.",
			usd(price), d.Name, payFix(cause), d.ExpiresAt.Format("Jan 2, 2006")),
		payLink, "store_renew_unpaid_"+d.ID.String())
	return errx.NewWithIdentifier(errx.PaymentRequired, CodeInsufficientBalance, "Your balance can't cover this renewal.")
}

// paymentFailed reports a charge the card refused or could not make; anything
// else (a payment still going through, an outage) is retried under the same key.
func paymentFailed(err error) bool {
	var card *CardError
	var method *PaymentMethodError
	return errors.As(err, &card) || errors.As(err, &method) || errors.Is(err, credits.ErrInsufficientCredits)
}

// payFix says what fixes a failed charge; updating the card charges it at once.
func payFix(cause error) string {
	var card *CardError
	var method *PaymentMethodError
	switch {
	case errors.As(cause, &card) && card.Code == "authentication_required":
		return "your bank asked to confirm the payment. Update or confirm your card and we charge it right away."
	case errors.As(cause, &card) && card.Code == models.ReloadPaymentFailed:
		return "the card payment could not be completed. Check your card and we charge it right away."
	case errors.As(cause, &card):
		return "your card was declined. Update your card and we charge it right away."
	case errors.As(cause, &method):
		return "there is no card on file. Add a card and we charge it right away."
	}
	return "the payment did not go through. Update your card and we charge it right away."
}

// RetryUnpaid charges a workspace's unpaid mailbox months and renewals again
// now, after its card changed, instead of waiting for tomorrow's attempt.
func (s *Service) RetryUnpaid(ctx context.Context, orgID uuid.UUID) error {
	if !s.Enabled() || !s.metered() {
		return nil
	}
	if err := s.Repo.ClearBillingRetry(ctx, orgID); err != nil {
		return err
	}
	return errors.Join(s.billOrg(ctx, orgID, true), s.renewDue(ctx))
}

// noticeDomains tells the workspace what is coming for each domain, once per
// expiry and stage, with the renewal price as it is now.
func (s *Service) noticeDomains(ctx context.Context) error {
	now := s.now()
	list, err := s.Repo.DomainsExpiringBefore(ctx, now.Add(renewAhead+renewNoticeLead), 500)
	if err != nil {
		return err
	}
	for i := range list {
		d := &list[i]
		key, title, body, upcoming := s.domainNotice(d, now)
		if key == "" {
			continue
		}
		price := s.livePrice(ctx, d)
		body = fmt.Sprintf(body, usd(price)) + priceChange(int64(d.LastSeenRenewMicros), price)
		won, err := s.Repo.SetDomainNotice(ctx, d.ID, key, models.Micro(price), upcoming)
		if err != nil || !won {
			continue
		}
		s.notify(ctx, d.OrganizationID, title, body, domainsLink, "store_domain_"+d.ID.String())
	}
	return nil
}

// domainNotice picks the notice a domain is due, if any. body carries one %s for the price.
func (s *Service) domainNotice(d *models.StoreDomain, now time.Time) (key, title, body string, upcoming bool) {
	exp := *d.ExpiresAt
	stamp := strconv.FormatInt(exp.Unix(), 10)
	left := exp.Sub(now)
	if d.AutoRenew && d.RenewalFailures == 0 {
		if d.RenewalNoticeAt != nil || left > renewAhead+renewNoticeLead {
			return "", "", "", false
		}
		charge := exp.Add(-renewAhead)
		if charge.Before(now) {
			charge = now
		}
		return "upcoming:" + stamp, d.Name + " renews on " + charge.Format("Jan 2"),
			d.Name + " renews on " + charge.Format("Jan 2, 2006") + " for %s, charged to your balance for one more year. Turn off auto-renew on the domain if you don't want it.", true
	}
	for _, days := range []int{1, 7, 30} {
		if left <= time.Duration(days)*24*time.Hour {
			stage := "expiry:" + strconv.Itoa(days) + ":" + stamp
			if d.LastNotice == stage {
				return "", "", "", false
			}
			when := "in " + strconv.Itoa(days) + " days"
			if days == 1 {
				when = "tomorrow"
			}
			return stage, d.Name + " expires " + when,
				d.Name + " expires on " + exp.Format("Jan 2, 2006") + ". Renew it for %s to keep it and its mailboxes working.", false
		}
	}
	return "", "", "", false
}

// livePrice is the domain's current renewal price at retail, falling back to the cached one.
func (s *Service) livePrice(ctx context.Context, d *models.StoreDomain) int64 {
	if live, err := s.api().GetDomain(ctx, d.GraphKitDomainID); err == nil {
		if row, err := s.upsertDomain(ctx, d.OrganizationID, nil, live); err == nil && row != nil {
			return int64(row.RenewRetailMicros)
		}
		return retailDomain(s.retail(ctx), live.RenewMicros)
	}
	return int64(d.RenewRetailMicros)
}

func priceChange(before, now int64) string {
	if before > 0 && now > before {
		return " The renewal price went up from " + usd(before) + " to " + usd(now) + "."
	}
	return ""
}
