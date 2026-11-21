package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/app/opsnotify"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// ErrBadSignature refuses a webhook GraphKit did not sign.
var ErrBadSignature = errors.New("store: webhook signature invalid")

// VerifyWebhook checks a delivery's signature and parses its envelope.
func (s *Service) VerifyWebhook(header string, body []byte) (*graphkit.Event, error) {
	if err := graphkit.VerifySignature(header, body, s.webhookSecrets(), s.now()); err != nil {
		return nil, ErrBadSignature
	}
	var ev graphkit.Event
	if err := json.Unmarshal(body, &ev); err != nil || ev.ID == "" {
		return nil, ErrBadSignature
	}
	return &ev, nil
}

// ApplyEvent brings the purchase an event describes up to date. The webhook,
// the reconciler and the simulator all deliver through it. Delivery is at
// least once and unordered; data is the current object, so applying it twice
// or late changes nothing, and the dedupe table only saves the work.
func (s *Service) ApplyEvent(ctx context.Context, ev graphkit.Event) error {
	if !s.Enabled() || ev.ID == "" {
		return nil
	}
	if seen, err := s.Repo.WebhookEventSeen(ctx, ev.ID); err == nil && seen {
		return nil
	}
	if err := s.apply(ctx, ev); err != nil {
		if errors.Is(err, errNotYet) {
			return nil
		}
		return err
	}
	_, err := s.Repo.RecordWebhookEvent(ctx, ev.ID)
	return err
}

// errNotYet is an event about an order this instance has not recorded as
// placed yet; it is left unrecorded so a redelivery or the feed applies it.
var errNotYet = errors.New("store: order not placed here yet")

func (s *Service) apply(ctx context.Context, ev graphkit.Event) error {
	switch ev.Type {
	case graphkit.EventPartnerBalanceLow:
		log.Warn().Msg("store: the instance's GraphKit partner balance is low; orders and renewals stop when it runs out")
		s.recordAlert(ev.Type, "GraphKit partner balance is low")
		if s.Ops != nil {
			s.Ops.NotifyOperator(opsnotify.EventStoreUnavailable, "GraphKit partner balance is low",
				"The GraphKit partner balance fell below its threshold. Orders and renewals are refused once it runs out; top it up.", nil)
		}
		return nil
	case graphkit.EventPricingUpdated, graphkit.EventSendingUpdated:
		// Prices are read live at every quote and renewal, and each domain's renewal price arrives as domain.updated.
		return nil
	}
	if ev.AccountExternalID == nil {
		return nil
	}
	orgID, err := uuid.Parse(*ev.AccountExternalID)
	if err != nil {
		return nil
	}
	if strings.HasPrefix(ev.Type, "social.") {
		// The add-on ignores a workspace it never set up here, as for another instance on the same key.
		if sc := s.socialAddon(); sc != nil {
			return sc.ApplySocialEvent(ctx, orgID, ev)
		}
		return nil
	}
	// An account this instance never opened is another instance's on the same partner key.
	acct, err := s.Repo.GetAccount(ctx, orgID)
	if err != nil {
		return err
	}
	if acct == nil {
		return nil
	}
	switch {
	case strings.HasPrefix(ev.Type, "order."):
		var o graphkit.Order
		if err := json.Unmarshal(ev.Data, &o); err != nil {
			return nil
		}
		row, err := s.Repo.GetOrderByGraphKitID(ctx, orgID, o.ID)
		if err != nil {
			return err
		}
		if row == nil {
			// The order call is still answering; the reconciler refreshes the order either way.
			return errNotYet
		}
		return s.applyOrder(ctx, row, &o)
	case strings.HasPrefix(ev.Type, "domain."):
		var d graphkit.Domain
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return nil
		}
		row, err := s.upsertDomain(ctx, orgID, nil, &d)
		if err != nil || row == nil {
			return err
		}
		s.domainNoticeFor(ctx, ev.Type, row, &d)
		return nil
	case strings.HasPrefix(ev.Type, "mailbox."):
		var m graphkit.Mailbox
		if err := json.Unmarshal(ev.Data, &m); err != nil {
			return nil
		}
		row, err := s.upsertMailbox(ctx, orgID, nil, &m, 0)
		if err != nil || row == nil {
			return err
		}
		s.syncSignIn(ctx, row, &m)
		s.connectLater(ctx, row, &m)
	}
	return nil
}

// domainNoticeFor tells the workspace what a domain event asks of it.
func (s *Service) domainNoticeFor(ctx context.Context, typ string, row *models.StoreDomain, d *graphkit.Domain) {
	orgID, name := row.OrganizationID, row.Name
	group := "store_domain_" + row.ID.String()
	switch typ {
	case graphkit.EventDomainVerificationRequired:
		v := d.RegistrantVerification
		if v == nil || v.Status != "pending" {
			return
		}
		by := ""
		if v.Deadline != nil {
			by = " before " + v.Deadline.Format("Jan 2, 2006")
		}
		s.notifyMailboxes(ctx, orgID, models.NotifDomainAuth, "Confirm the owner email for "+name,
			"We sent a message to "+v.Email+" to confirm the owner of "+name+". Open it and confirm"+by+", or the domain may stop working.",
			domainsLink, group+"_verify")
	case graphkit.EventDomainTransferRequested:
		when := ""
		if d.Transfer != nil && d.Transfer.AutoApproveAt != nil {
			when = " It completes on " + d.Transfer.AutoApproveAt.Format("Jan 2, 2006") + " unless it is cancelled at the new registrar."
		}
		s.notify(ctx, orgID, name+" is being transferred", "Another registrar asked for "+name+"."+when, domainsLink, group+"_transfer")
	case graphkit.EventDomainConnected:
		if row.Source == models.StoreDomainConnected {
			s.notifyMailboxes(ctx, orgID, models.NotifDomainAuth, name+" is connected",
				name+" now points at Warmbly. You can buy mailboxes on it.", domainsLink, group+"_connected")
		}
	case graphkit.EventDomainDisconnected:
		if row.Source != models.StoreDomainConnected {
			return
		}
		switch d.ConnectionStatus {
		case graphkit.ConnectionMoved:
			s.notifyMailboxes(ctx, orgID, models.NotifDomainAuth, name+" stopped pointing at Warmbly",
				"The nameservers or records for "+name+" changed, so its mail records may stop working. Put them back as shown on the domain.", domainsLink, group+"_moved")
		case graphkit.ConnectionExpired:
			s.notifyMailboxes(ctx, orgID, models.NotifDomainAuth, "Connecting "+name+" timed out",
				"We did not see the nameservers or records for "+name+" within 48 hours. Start again from the domain.", domainsLink, group+"_expired")
		}
	}
}

// Reconcile is one reconciler pass.
func (s *Service) Reconcile(ctx context.Context) error {
	var errs []error
	errs = append(errs, s.drainEvents(ctx), s.refreshOpenOrders(ctx), s.recoverOrders(ctx), s.retryConnects(ctx), s.retireEnded(ctx), s.setupTrackingDue(ctx),
		s.retryPhotos(ctx), s.sweepPhotos(ctx), s.retrySignatures(ctx), s.reconcileSocial(ctx))
	return errors.Join(errs...)
}

func (s *Service) drainEvents(ctx context.Context) error {
	cursor, err := s.Repo.GetEventsCursor(ctx)
	if err != nil {
		return err
	}
	for range 20 {
		page, gerr := s.api().Events(ctx, cursor, 100)
		if gerr != nil {
			if ge, ok := graphkit.AsError(gerr); ok && ge.Code == graphkit.CodeInvalidRequest && cursor != "" {
				// The cursor aged out of the 30-day feed: start over; applying is idempotent.
				log.Warn().Msg("store: events cursor expired; replaying the feed")
				return s.Repo.SetEventsCursor(ctx, "")
			}
			logUpstream("events", gerr)
			return nil
		}
		for _, ev := range page.Data {
			if err := s.ApplyEvent(ctx, ev); err != nil {
				// Stop here so the event is read again next pass.
				return err
			}
			cursor = ev.ID
		}
		if len(page.Data) > 0 {
			if err := s.Repo.SetEventsCursor(ctx, cursor); err != nil {
				return err
			}
		}
		if !page.Pagination.HasMore {
			return nil
		}
	}
	return nil
}

// openOrderAge is how long an order may sit in flight before the reconciler asks GraphKit about it.
const openOrderAge = 2 * time.Minute

// slowOrderEvery is how often an order still setting up after its first hour is refreshed.
const slowOrderEvery = 15 * time.Minute

func (s *Service) refreshOpenOrders(ctx context.Context) error {
	// Where nothing is charged, an unplaced order has nothing to settle first and is placed again.
	rows, err := s.Repo.ListOpenOrders(ctx, s.now().Add(-openOrderAge), 100, !s.metered())
	if err != nil {
		return err
	}
	now := s.now()
	for i := range rows {
		row := &rows[i]
		// Live mailbox setup can take days; an order past its first hour is asked about every quarter hour.
		if row.GraphKitOrderID != nil && now.Sub(row.CreatedAt) > time.Hour && now.Sub(row.UpdatedAt) < slowOrderEvery {
			continue
		}
		var snap quoteSnapshot
		if err := json.Unmarshal(row.Quote, &snap); err != nil {
			continue
		}
		if row.GraphKitOrderID == nil {
			// Charged but never placed: place it now under the same key, or refund it.
			if _, err := s.submit(ctx, row, snap); err != nil {
				log.Warn().Err(err).Str("order_id", row.ID.String()).Msg("store: placing a charged order failed")
			}
			continue
		}
		o, gerr := s.api().GetOrder(ctx, externalID(row.OrganizationID), *row.GraphKitOrderID)
		if gerr != nil {
			logUpstream("order.refresh", gerr)
			continue
		}
		if err := s.applyOrder(ctx, row, o); err != nil {
			log.Warn().Err(err).Str("order_id", row.ID.String()).Msg("store: refreshing an order failed")
		}
	}
	return nil
}

// strandedOrderAge is how long an order only partly charged, or not at all, may sit
// before the reconciler refunds or removes it; any request is long over by then. One
// whose every line was taken is placed sooner, since placing it again is harmless.
const strandedOrderAge = 30 * time.Minute

// recoverOrders finishes what a request left behind: an order whose lines were
// debited but never recorded is placed (or refunded when only part was taken),
// one that took nothing is removed, and a refund that did not go through is retried.
func (s *Service) recoverOrders(ctx context.Context) error {
	if !s.metered() {
		return nil
	}
	now := s.now()
	stranded, err := s.Repo.StrandedOrders(ctx, now.Add(-openOrderAge), 100)
	if err != nil {
		return err
	}
	var errs []error
	for i := range stranded {
		errs = append(errs, s.recoverStranded(ctx, &stranded[i]))
	}
	pending, err := s.Repo.RefundPendingOrders(ctx, now.Add(-openOrderAge), 100)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for i := range pending {
		row := &pending[i]
		var snap quoteSnapshot
		if err := json.Unmarshal(row.Quote, &snap); err != nil {
			continue
		}
		if back, ok := s.refundLines(ctx, row, snap); ok {
			errs = append(errs, s.Repo.MarkOrderRefunded(ctx, row.OrganizationID, row.ID, back))
		}
	}
	return errors.Join(errs...)
}

// recoverStranded settles one order from what the ledger holds for its lines.
func (s *Service) recoverStranded(ctx context.Context, row *models.StoreOrder) error {
	var snap quoteSnapshot
	if err := json.Unmarshal(row.Quote, &snap); err != nil {
		return nil
	}
	var taken models.Micro
	lines, debited := 0, 0
	for i, l := range snap.Lines {
		if l.AmountMicros <= 0 {
			continue
		}
		lines++
		prior, err := s.Credits.ChargeOf(ctx, row.OrganizationID, lineKey(row.ChargeKey, i))
		if err != nil {
			return err
		}
		if prior != nil && prior.Refunded < prior.Taken {
			debited++
			taken += prior.Taken
		}
	}
	switch {
	case debited < lines && s.now().Sub(row.UpdatedAt) < strandedOrderAge:
		// A request may still be charging it.
		return nil
	case debited == 0:
		// The checkout took nothing: the buyer can start again.
		return s.Repo.DeleteUnchargedOrder(ctx, row.OrganizationID, row.ID)
	case debited < lines:
		back, ok := s.refundLines(ctx, row, snap)
		return s.Repo.MarkOrderFailed(ctx, row.OrganizationID, row.ID, CodeUnavailable, back, !ok)
	}
	if err := s.Repo.MarkOrderCharged(ctx, row.OrganizationID, row.ID, taken); err != nil {
		return err
	}
	row.ChargedMicros = taken
	var price *PriceChangedError
	if _, err := s.submit(ctx, row, snap); err != nil && !errors.As(err, &price) {
		return err
	}
	// A quote that expired meanwhile: submit refunded the order and marked it failed.
	return nil
}

// ---- delegation ---------------------------------------------------------

// PushDelegation registers this instance's identities with GraphKit: its
// Google service account, authorized for domain-wide delegation in every
// purchased Workspace, and its Microsoft app, consented in every purchased
// tenant. An identity the instance lacks is left out; without a Google service
// account each workspace's account carries the broker's scopes instead
// (syncIdentity). The key is the body's digest, so a restart repeats nothing.
func (s *Service) PushDelegation(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	var d graphkit.Delegation
	if s.Delegator != nil {
		if id, scopes, ok := s.Delegator.GoogleAppIdentity(); ok {
			d.Google = &graphkit.GoogleDelegation{ClientID: id, Scopes: scopes}
			// New Workspaces connect by sign-in to this instance's OAuth client: new domain-wide delegation
			// grants are retired. Not in the sandbox or simulator, where no vendor team signs anything in.
			if !s.Simulate && s.links != nil {
				d.Google.SignInClientID = config.GoogleOAuthClientID()
			}
		}
		if app, perms, ok := s.Delegator.MicrosoftAppIdentity(); ok {
			d.Microsoft = &graphkit.MicrosoftDelegation{AppID: app, Permissions: perms}
		}
	}
	if d.Empty() {
		return nil
	}
	err := s.putDelegation(ctx, d)
	if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeInvalidRequest && d.Google != nil && d.Microsoft != nil {
		// A refused Microsoft identity must not keep Google mailboxes from connecting.
		logUpstream("delegation.microsoft", err)
		err = s.putDelegation(ctx, graphkit.Delegation{Google: d.Google})
	}
	if err != nil {
		logUpstream("delegation", err)
	}
	return err
}

func (s *Service) putDelegation(ctx context.Context, d graphkit.Delegation) error {
	raw, _ := json.Marshal(d)
	return s.api().PutDelegation(ctx, d, "store:delegation:"+digest(raw)+":"+s.now().Format("2006-01-02"))
}

// GoogleToken brokers a Google access token for a purchased mailbox through
// GraphKit. The caller caches it below its expiry; each call is a fresh mint.
func (s *Service) GoogleToken(ctx context.Context, orgID uuid.UUID, email string, scopes []string) (*oauth2.Token, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	m, err := s.Repo.GetMailboxByEmail(ctx, orgID, strings.ToLower(email))
	if err != nil {
		return nil, err
	}
	if m == nil || m.Provider != graphkit.ProviderGoogle || m.ConnectMethod != graphkit.ConnectToken {
		return nil, errors.New("store: no brokered mailbox for this address")
	}
	if m.Status != graphkit.MailboxActive && m.Status != graphkit.MailboxCancelling {
		return nil, errors.New("store: the mailbox is not active")
	}
	tok, gerr := s.api().MailboxAccessToken(ctx, m.GraphKitMailboxID, scopes, "store:token:"+uuid.NewString())
	if gerr != nil {
		logUpstream("mailbox.token", gerr)
		return nil, gerr
	}
	return &oauth2.Token{AccessToken: tok.AccessToken, TokenType: "Bearer", Expiry: tok.ExpiresAt}, nil
}

// Simulated reports the development simulator or sandbox: its tokens are opaque, so a
// connect proves nothing against Google. Never true outside development.
func (s *Service) Simulated() bool { return s.simulating() && config.DevEnv() }

var _ delegation.PurchasedTokens = (*Service)(nil)

// RetailPricing is GraphKit's live price list with this instance's retail
// alongside, for the admin panel's preview.
type RetailPricing struct {
	*graphkit.Pricing
	Retail []RetailTLD `json:"retail"`
}

// RetailTLD is one TLD at the instance's retail.
type RetailTLD struct {
	TLD            string `json:"tld"`
	RegisterMicros int64  `json:"register_micros"`
	RenewMicros    int64  `json:"renew_micros"`
}

// AdminPricing reads GraphKit's current price list.
func (s *Service) AdminPricing(ctx context.Context) (*RetailPricing, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	p, gerr := s.api().Pricing(ctx)
	if gerr != nil {
		return nil, s.upstream(ctx, "pricing", gerr)
	}
	r := s.retail(ctx)
	out := &RetailPricing{Pricing: p, Retail: []RetailTLD{}}
	for _, d := range p.Domains {
		out.Retail = append(out.Retail, RetailTLD{TLD: d.TLD, RegisterMicros: retailDomain(r, d.RegisterMicros), RenewMicros: retailDomain(r, d.RenewMicros)})
	}
	return out, nil
}
