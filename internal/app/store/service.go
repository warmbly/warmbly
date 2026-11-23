// Package store sells domains and Google Workspace / Microsoft 365 mailboxes
// inside Warmbly, fulfilled through the instance's GraphKit partner account.
// Everything a workspace sees is Warmbly's: its ids, its codes, its copy, its
// balance and ledger. GraphKit ids and wording stay inside this package.
package store

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/balance"
	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// Credits is the slice of the workspace balance the store charges.
type Credits interface {
	Unmetered() bool
	GetBalance(ctx context.Context, orgID uuid.UUID) (models.Micro, *errx.Error)
	// GetPurchasedBalance is the money the workspace added, the only pool a store charge draws on.
	GetPurchasedBalance(ctx context.Context, orgID uuid.UUID) (models.Micro, *errx.Error)
	ChargeAmount(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, error)
	RefundCharge(ctx context.Context, orgID uuid.UUID, chargeKey, reason string) (models.Micro, error)
	// ChargeOf is the debit already taken under key, nil when there is none: a charge on the ledger is never collected again.
	ChargeOf(ctx context.Context, orgID uuid.UUID, key string) (*models.KeyedCharge, error)
	// RefundStore gives a store charge back once under refundKey, never past what the debits under capPrefix took.
	RefundStore(ctx context.Context, orgID uuid.UUID, capPrefix, spendKey, refundKey string, amount models.Micro, reason string) (models.Micro, error)
}

// Cards charges the saved card for what the balance does not cover, keyed by
// the store charge so a retry never charges the card twice.
type Cards interface {
	ChargeStore(ctx context.Context, orgID uuid.UUID, amount models.Micro, key, description string, offSession bool) (*balance.TopUpResult, error)
	State(ctx context.Context, orgID uuid.UUID) (*balance.State, error)
	// SetAutoReload is the balance's automatic reload, which the store sizes to the mailboxes.
	SetAutoReload(ctx context.Context, orgID uuid.UUID, enabled bool, threshold, amount, monthlyLimit models.Micro) (*models.AISpendSettings, error)
}

// Retail is the instance's store price list.
type Retail interface {
	StoreRetail(ctx context.Context) instancesettings.StoreRetail
}

// BillingDetailsSource reads the workspace's billing contact, for the registrant prefill.
type BillingDetailsSource interface {
	BillingDetails(ctx context.Context, orgID uuid.UUID) (*BillingDetails, error)
}

// People resolves the user and workspace a purchase belongs to.
type People interface {
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetOrganization(ctx context.Context, id uuid.UUID) (*models.Organization, error)
}

// Delegator connects purchased mailboxes without a password: brokered through
// the store's token broker, or through this instance's own identity, with
// admin naming the domain's admin mailbox for a grant made on its Workspace.
type Delegator interface {
	ConnectPurchased(ctx context.Context, orgID, userID uuid.UUID, provider, address, name, admin string, brokered bool) (*models.Email, *errx.Error)
	MicrosoftAppIdentity() (appID string, permissions []string, ok bool)
	// ConsentPurchasedTenantWithAdmin approves the app in a purchased domain's tenant with its admin mailbox's sign-in;
	// a *delegation.AutoConsentError says why it did not.
	ConsentPurchasedTenantWithAdmin(ctx context.Context, orgID, userID uuid.UUID, domain, adminEmail, adminPassword string) error
	// StartPurchasedMicrosoftConsent opens a purchased domain's Microsoft 365 tenant consent, ending at returnURL.
	StartPurchasedMicrosoftConsent(ctx context.Context, orgID, userID uuid.UUID, domain, returnURL string) (*delegation.MicrosoftConsent, *errx.Error)
	// GoogleAppIdentity is this instance's service account for domain-wide delegation; ok is false without one.
	GoogleAppIdentity() (clientID string, scopes []string, ok bool)
	// SetPurchasedSignature sets the Gmail send-as signature on a purchased Google mailbox's own address.
	SetPurchasedSignature(ctx context.Context, orgID uuid.UUID, address, signatureHTML string, brokered bool) error
}

// PlanGate reads whether a workspace has a paid plan in force; a self-host always has.
type PlanGate interface {
	HasPremiumWarmup(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// ManagedOrgs reports a workspace a partner manages: the partner buys and
// attaches its mailboxes, so the store is off there and the plan rule does not apply.
type ManagedOrgs interface {
	PartnerManaged(ctx context.Context, orgID uuid.UUID) bool
}

// OperatorAlerts reaches the instance operator, for what no workspace can fix.
type OperatorAlerts interface {
	NotifyOperator(key, title, summary string, fields map[string]string)
}

// SendingDomains sets up a purchased domain's tracking host, through the store's own DNS.
type SendingDomains interface {
	VendorTracking(ctx context.Context, orgID uuid.UUID, domain, host string) (*models.TrackingDomainStatus, int, *errx.Error)
}

// Mailboxes is the normal mailbox connect path, for the password fallback, warmup, and switching a mailbox off.
type Mailboxes interface {
	OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)
	SetWarmupLifecycle(ctx context.Context, orgID, emailAccountID, action string) (*models.Email, *errx.Error)
	Update(ctx context.Context, orgID, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	// UpdateTrackingDomain points one mailbox at a tracking host.
	UpdateTrackingDomain(ctx context.Context, orgID, emailAccountID, domain string) (*models.TrackingDomainStatus, *errx.Error)
}

// Notifier tells whoever manages billing, in the app and by email.
type Notifier interface {
	NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)
}

// Publisher fans store changes out to the workspace's dashboards.
type Publisher interface {
	PublishStoreEvent(ctx context.Context, orgID uuid.UUID, ev pubsub.StoreEvent)
}

// Deps builds a Service. API, WebhookSecrets and Simulate are the connection it starts on; Connect replaces it.
type Deps struct {
	API       graphkit.API
	Repo      repository.StoreRepository
	Credits   Credits
	Cards     Cards
	Retail    Retail
	Billing   BillingDetailsSource
	People    People
	Delegator Delegator
	Mailboxes Mailboxes
	Notifier  Notifier
	Publisher Publisher
	Plans     PlanGate
	Managed   ManagedOrgs
	Ops       OperatorAlerts
	// Blobs keeps uploaded mailbox photos; nil turns photos off.
	Blobs PhotoBlobs
	// Websites is the workspace's own website, the default a new domain forwards to.
	Websites WorkspaceWebsites
	// WebhookSecrets verify GraphKit's signature; the previous one keeps verifying through a rotation.
	WebhookSecrets []string
	// Simulate marks the in-process simulator or GraphKit's sandbox, whose tokens Google refuses.
	Simulate bool
	// SelfHosted is the instance's explicit deployment mode; only a self-host shows an unconfigured store as locked.
	SelfHosted bool
	Now        func() time.Time
}

// Service is the store.
type Service struct {
	Deps
	// connects tracks mailbox connects running off a request.
	connects sync.WaitGroup
	// domains is wired after construction: the sending-domain service takes the store as a vendor.
	domains SendingDomains

	mu      sync.Mutex
	pricing *graphkit.Pricing
	priced  time.Time
	alerted map[string]time.Time
	// trackingTried spaces out tracking host setups that failed.
	trackingTried map[uuid.UUID]time.Time
	// managedOrgs is the partner core, wired after construction; Deps.Managed stands in until then.
	managedOrgs ManagedOrgs
	// avatars shows mailbox photos in the dashboard, wired after construction.
	avatars MailboxAvatars

	// up is the GraphKit connection in use; read it through api, never through Deps.api().
	up      atomic.Pointer[upstream]
	changed chan struct{}
	base    context.Context
	// settings is the admin panel's stored connection, wired after construction.
	settings ConnectionSettings
	envCfg   config.GraphKit
	// links and signIns mint and remember purchased mailboxes' sign-in links (signin.go), wired after construction.
	links   ConnectLinks
	signIns repository.StoreSignInLinkRepository
	// social is the social add-on (social.go), wired after construction.
	social Social
}

// WireManaged attaches the partner core's check, built after the store.
func (s *Service) WireManaged(m ManagedOrgs) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.managedOrgs = m
	s.mu.Unlock()
}

// WireSendingDomains attaches the sending-domain service, which sets up tracking hosts.
func (s *Service) WireSendingDomains(d SendingDomains) {
	if s != nil {
		s.domains = d
	}
}

// New builds the store.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &Service{Deps: d, changed: make(chan struct{}, 1)}
	first := &upstream{api: d.API, on: d.API != nil, secrets: nonEmpty(d.WebhookSecrets...), simulate: d.Simulate, source: SourceEnvironment}
	if d.API == nil {
		first.source = SourceNone
	}
	s.up.Store(first)
	s.Deps.API, s.Deps.WebhookSecrets, s.Deps.Simulate = nil, nil, false
	return s
}

// Enabled reports whether the instance sells anything.
func (s *Service) Enabled() bool {
	u := s.current()
	return u != nil && u.on && s.Repo != nil
}

// readable reports purchases that can be listed from what is recorded here, connected to GraphKit or not.
func (s *Service) readable() bool { return s != nil && s.Repo != nil }

func (s *Service) now() time.Time { return s.Now().UTC() }

func (s *Service) metered() bool { return s.Credits != nil && !s.Credits.Unmetered() }

func (s *Service) retail(ctx context.Context) instancesettings.StoreRetail {
	if s.Retail == nil {
		r := instancesettings.DefaultStoreRetail()
		return r
	}
	return s.Retail.StoreRetail(ctx)
}

func (s *Service) publish(ctx context.Context, orgID uuid.UUID, ev pubsub.StoreEvent) {
	if s.Publisher != nil {
		s.Publisher.PublishStoreEvent(context.WithoutCancel(ctx), orgID, ev)
	}
}

// notify tells whoever manages billing: payments, renewals, plans.
func (s *Service) notify(ctx context.Context, orgID uuid.UUID, title, body, link, group string) {
	if s.Notifier == nil {
		return
	}
	s.Notifier.NotifyOrg(context.WithoutCancel(ctx), orgID, models.PermManageBilling, uuid.Nil, models.NotifBillingAlert, title, body, link, nil, group)
}

// notifyMailboxes tells whoever manages mailboxes: order setup, domain connections, owner checks.
func (s *Service) notifyMailboxes(ctx context.Context, orgID uuid.UUID, cat models.NotificationCategory, title, body, link, group string) {
	if s.Notifier == nil {
		return
	}
	s.Notifier.NotifyOrg(context.WithoutCancel(ctx), orgID, models.PermManageEmails, uuid.Nil, cat, title, body, link, nil, group)
}

// CodePlanRequired refuses a purchase from a workspace without a paid plan.
const CodePlanRequired = "plan_required"

// CodeManagedByPartner refuses the store in a workspace a partner manages.
const CodeManagedByPartner = "store_managed_by_partner"

// managed reports a workspace whose mailboxes a partner buys for it.
func (s *Service) managed(ctx context.Context, orgID uuid.UUID) bool {
	s.mu.Lock()
	m := s.managedOrgs
	s.mu.Unlock()
	if m == nil {
		m = s.Managed
	}
	return m != nil && m.PartnerManaged(ctx, orgID)
}

// planActive reports a paid plan in force. Without a gate (a self-host) every workspace has one.
func (s *Service) planActive(ctx context.Context, orgID uuid.UUID) (bool, error) {
	if s.Plans == nil {
		return true, nil
	}
	ok, xerr := s.Plans.HasPremiumWarmup(ctx, orgID)
	if xerr != nil {
		return false, xerr
	}
	return ok, nil
}

// requireBuyer refuses a purchase the workspace may not make: a partner-managed
// workspace buys through its partner, and every other one needs a paid plan.
func (s *Service) requireBuyer(ctx context.Context, orgID uuid.UUID) error {
	if !s.Enabled() {
		return errDisabled
	}
	if s.managed(ctx, orgID) {
		return errManaged
	}
	if s.retail(ctx).SalesPaused {
		return errSalesPaused
	}
	ok, err := s.planActive(ctx, orgID)
	if err != nil {
		return err
	}
	if !ok {
		return errx.NewWithIdentifier(errx.PaymentRequired, CodePlanRequired, "Buying domains and mailboxes needs a Warmbly plan. Choose a plan to continue.")
	}
	return nil
}

// livePricing is GraphKit's price list, read at most every ten minutes.
func (s *Service) livePricing(ctx context.Context) (*graphkit.Pricing, error) {
	s.mu.Lock()
	if s.pricing != nil && s.now().Sub(s.priced) < 10*time.Minute {
		p := s.pricing
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	p, err := s.api().Pricing(pctx)
	if err != nil {
		s.alertIfAccountLevel(err)
		return nil, err
	}
	s.mu.Lock()
	s.pricing, s.priced = p, s.now()
	s.mu.Unlock()
	return p, nil
}

// externalID is the workspace's id at GraphKit.
func externalID(orgID uuid.UUID) string { return orgID.String() }

// defaultLimits are GraphKit's caps when the price list cannot be read.
var defaultLimits = graphkit.Limits{MailboxesPerDomainMax: 5, MailboxesPerDomainRecommended: 3}

// Status is what the dashboard needs to offer the store at all.
func (s *Service) Status(ctx context.Context, orgID uuid.UUID) (*Status, error) {
	off := &Status{Enabled: false, Configured: s.Enabled(), Currency: "USD", Providers: []string{}, Limits: defaultLimits}
	if s.managed(ctx, orgID) {
		off.Reason = ReasonPartnerManaged
		return off, nil
	}
	if !s.Enabled() {
		off.Reason = ReasonNotConfigured
		off.Locked = s.SelfHosted
		d, err := s.PurchasesWithoutConnection(ctx, &orgID)
		if err != nil {
			return nil, err
		}
		off.Disconnected = d
		return off, nil
	}
	r := s.retail(ctx)
	out := &Status{Enabled: true, Configured: true, SalesPaused: r.SalesPaused, Metered: s.metered(), Currency: "USD", MailboxMonthlyMicros: r.MailboxMonthly(),
		DomainMarkup: "at_cost", Limits: defaultLimits, Providers: []string{graphkit.ProviderGoogle, graphkit.ProviderMicrosoft}}
	if !atCost(r) {
		out.DomainMarkup = "markup"
	}
	if p, err := s.livePricing(ctx); err == nil {
		if p.Limits.MailboxesPerDomainMax > 0 {
			out.Limits = p.Limits
		}
	}
	active, err := s.planActive(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out.PlanActive = active
	out.MinCardChargeMicros = r.MinCardCharge()
	if acct, err := s.Repo.GetAccount(ctx, orgID); err == nil && acct != nil && acct.BillDay > 0 {
		day := acct.BillDay
		out.BillingDay = &day
		out.NextBundle = s.nextBundle(ctx, acct)
	}
	if out.Metered {
		bal, card := s.balanceAndCard(ctx, orgID)
		out.Balance = &BalanceView{BalanceMicros: bal, Card: card}
	}
	reg, err := s.savedRegistrant(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out.Registrant = reg
	if reg == nil {
		p := s.prefill(ctx, orgID)
		out.RegistrantPrefill = &p
	}
	return out, nil
}

// balanceAndCard is the money the workspace added (never the plan's AI
// allowance or an agency's AI funding) and the saved card.
func (s *Service) balanceAndCard(ctx context.Context, orgID uuid.UUID) (int64, *Card) {
	var bal int64
	if s.Credits != nil {
		if b, xerr := s.Credits.GetPurchasedBalance(ctx, orgID); xerr == nil {
			bal = int64(b)
		}
	}
	var card *Card
	if s.Cards != nil {
		if st, err := s.Cards.State(ctx, orgID); err == nil && st != nil && st.PaymentMethod != nil {
			card = &Card{Brand: st.PaymentMethod.Brand, Last4: st.PaymentMethod.Last4}
		}
	}
	return bal, card
}

func (s *Service) savedRegistrant(ctx context.Context, orgID uuid.UUID) (*Registrant, error) {
	acct, err := s.Repo.GetAccount(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if acct == nil || len(acct.Registrant) == 0 || string(acct.Registrant) == "null" {
		return nil, nil
	}
	var r Registrant
	if err := json.Unmarshal(acct.Registrant, &r); err != nil {
		return nil, nil
	}
	return &r, nil
}

// prefill drafts a registrant from Stripe's billing details, the workspace and its owner.
func (s *Service) prefill(ctx context.Context, orgID uuid.UUID) Registrant {
	var first, last, email, workspace string
	if s.People != nil {
		if org, err := s.People.GetOrganization(ctx, orgID); err == nil && org != nil {
			workspace = org.Name
			if u, err := s.People.GetUser(ctx, org.OwnerUserID); err == nil && u != nil {
				first, last, email = u.FirstName, u.LastName, u.Email
			}
		}
	}
	var b *BillingDetails
	if s.Billing != nil {
		if d, err := s.Billing.BillingDetails(ctx, orgID); err == nil {
			b = d
		}
	}
	return Prefill(b, first, last, email, workspace)
}

// PrefillFor drafts a registrant with the signed-in user's name, for the checkout card.
func (s *Service) PrefillFor(ctx context.Context, orgID, userID uuid.UUID) Registrant {
	r := s.prefill(ctx, orgID)
	if s.People != nil {
		if u, err := s.People.GetUser(ctx, userID); err == nil && u != nil {
			r.FirstName, r.LastName = u.FirstName, u.LastName
			if r.Email == "" {
				r.Email = u.Email
			}
		}
	}
	return r.Normalize()
}

// PutRegistrant validates and saves the registrant, at GraphKit and here.
func (s *Service) PutRegistrant(ctx context.Context, orgID uuid.UUID, in Registrant) (*Registrant, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	if s.managed(ctx, orgID) {
		return nil, errManaged
	}
	r, err := in.Validate()
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(r)
	acct, gerr := s.api().UpsertAccount(ctx, externalID(orgID), graphkit.AccountInput{Name: s.orgName(ctx, orgID), Registrant: r.contact()},
		"store:account:"+orgID.String()+":"+digest(raw))
	if gerr != nil {
		if ge, ok := graphkit.AsError(gerr); ok && ge.Code == graphkit.CodeContactInvalid {
			return nil, NewContactError(ge.ContactFields())
		}
		return nil, s.upstream(ctx, "registrant", gerr)
	}
	if err := s.Repo.UpsertAccount(ctx, orgID, acct.ID, raw); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Service) orgName(ctx context.Context, orgID uuid.UUID) string {
	if s.People != nil {
		if org, err := s.People.GetOrganization(ctx, orgID); err == nil && org != nil && org.Name != "" {
			return org.Name
		}
	}
	return orgID.String()
}

// ensureAccount makes sure the workspace has a GraphKit account.
func (s *Service) ensureAccount(ctx context.Context, orgID uuid.UUID) error {
	acct, err := s.Repo.GetAccount(ctx, orgID)
	if err != nil {
		return err
	}
	if acct == nil || acct.GraphKitAccountID == "" {
		in := graphkit.AccountInput{Name: s.orgName(ctx, orgID)}
		got, gerr := s.api().UpsertAccount(ctx, externalID(orgID), in, "store:account:"+orgID.String())
		if gerr != nil {
			return s.upstream(ctx, "account", gerr)
		}
		if err := s.Repo.UpsertAccount(ctx, orgID, got.ID, nil); err != nil {
			return err
		}
		acct = &models.StoreAccount{OrganizationID: orgID, GraphKitAccountID: got.ID}
	}
	err = s.pushIdentity(ctx, acct)
	if graphkit.CodeOf(err) == graphkit.CodeAccountNotFound {
		// The account we recorded is gone upstream (another partner environment, a reset simulator): open it again.
		return s.reopenAccount(ctx, orgID)
	}
	return s.identityErr(ctx, err)
}

// accountIdentity is what the workspace's account needs on top of the
// partner's identities: without this instance's own Google service account,
// its Google mailboxes connect through GraphKit's token broker, limited to the
// scopes a purchased mailbox runs on. Empty means the partner's apply.
func (s *Service) accountIdentity() graphkit.Delegation {
	if s.Delegator != nil {
		if _, _, ok := s.Delegator.GoogleAppIdentity(); ok {
			return graphkit.Delegation{}
		}
	}
	return graphkit.Delegation{Google: &graphkit.GoogleDelegation{Scopes: delegation.PurchasedGoogleScopes}}
}

// syncIdentity sets the account's own identities when they differ from what was last set.
func (s *Service) syncIdentity(ctx context.Context, acct *models.StoreAccount) error {
	return s.identityErr(ctx, s.pushIdentity(ctx, acct))
}

func (s *Service) identityErr(ctx context.Context, err error) error {
	if _, ok := graphkit.AsError(err); ok {
		return s.upstream(ctx, "account.identity", err)
	}
	return err
}

// pushIdentity is syncIdentity with GraphKit's refusal as it came.
func (s *Service) pushIdentity(ctx context.Context, acct *models.StoreAccount) error {
	want := s.accountIdentity()
	raw, _ := json.Marshal(want)
	sum := digest(raw)
	if acct.IdentityDigest == sum {
		return nil
	}
	// The account id is in the key: a reopened account is a new one, not a replay of the refusal.
	key := "store:identity:" + acct.OrganizationID.String() + ":" + acct.GraphKitAccountID + ":" + sum
	var gerr error
	if want.Empty() {
		gerr = s.api().DeleteAccountDelegation(ctx, externalID(acct.OrganizationID), key)
	} else {
		_, gerr = s.api().PutAccountDelegation(ctx, externalID(acct.OrganizationID), want, key)
	}
	if gerr != nil {
		return gerr
	}
	return s.Repo.SetIdentityDigest(ctx, acct.OrganizationID, sum)
}

// reopenAccount opens the workspace's GraphKit account again with the registrant on record.
func (s *Service) reopenAccount(ctx context.Context, orgID uuid.UUID) error {
	in := graphkit.AccountInput{Name: s.orgName(ctx, orgID)}
	reg, err := s.savedRegistrant(ctx, orgID)
	if err != nil {
		return err
	}
	if reg != nil {
		in.Registrant = reg.contact()
	}
	got, gerr := s.api().UpsertAccount(ctx, externalID(orgID), in, "store:account:"+orgID.String()+":"+uuid.NewString())
	if gerr != nil {
		return s.upstream(ctx, "account.reopen", gerr)
	}
	if err := s.Repo.UpsertAccount(ctx, orgID, got.ID, nil); err != nil {
		return err
	}
	// The identities lived on the old account; set them again.
	if err := s.Repo.SetIdentityDigest(ctx, orgID, ""); err != nil {
		return err
	}
	return s.syncIdentity(ctx, &models.StoreAccount{OrganizationID: orgID, GraphKitAccountID: got.ID})
}

// Search finds domains for a keyword or a name, at retail.
func (s *Service) Search(ctx context.Context, query string, tlds []string, exactOnly bool) (*SearchResult, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	q, err := cleanQuery(query)
	if err != nil {
		return nil, err
	}
	res, gerr := s.api().Search(ctx, q, cleanTLDs(tlds), 30, exactOnly)
	if gerr != nil {
		return nil, s.upstream(ctx, "search", gerr)
	}
	// A TLD that registers for more than a year is priced for its minimum term.
	prices, _ := s.livePricing(ctx)
	r := s.retail(ctx)
	out := &SearchResult{Query: query, Data: make([]Offer, 0, len(res.Data))}
	for _, o := range res.Data {
		off := Offer{Domain: o.Domain, DisplayName: o.DisplayName, TLD: o.TLD, Available: o.Available && !o.Premium, Exact: o.Exact,
			Reason: offerReason(o), Years: prices.MinYears(o.TLD)}
		if o.RegisterMicros != nil {
			off.PriceMicros = retailDomain(r, *o.RegisterMicros) * int64(off.Years)
		}
		if o.RenewMicros != nil {
			off.RenewMicros = retailDomain(r, *o.RenewMicros)
		}
		out.Data = append(out.Data, off)
	}
	return out, nil
}

// offerReason names why a domain cannot be bought, in Warmbly's terms.
func offerReason(o graphkit.DomainOffer) *string {
	if o.Available && !o.Premium {
		return nil
	}
	reason := "registered"
	if o.Reason != nil {
		switch *o.Reason {
		case "premium", "reserved", "tld_unsupported":
			reason = *o.Reason
		}
	}
	if o.Premium {
		reason = "premium"
	}
	return &reason
}

// logUpstream records GraphKit's refusal against our own context; its wording never reaches a caller.
func logUpstream(op string, err error) {
	ev := log.Warn().Str("store_op", op)
	if ge, ok := graphkit.AsError(err); ok {
		ev = ev.Str("upstream_code", ge.Code).Int("upstream_status", ge.Status).Str("upstream_request", ge.RequestID)
	} else {
		ev = ev.Err(err)
	}
	ev.Msg("store: upstream call failed")
}

// NewAPI builds the GraphKit client the configuration names: nil when the
// store is off, the in-process simulator for GRAPHKIT_API_URL=fake, which is
// refused outside development.
func NewAPI(cfg config.GraphKit) (graphkit.API, *graphkit.Fake, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if cfg.Fake() {
		f := graphkit.NewFake()
		// The simulator's GraphKit link page is a stand-in the backend serves itself.
		f.SetLinkPage(func(id string) string { return config.BackendPublicURL() + "/v1/social/sandbox/link/" + id })
		return f, f, nil
	}
	if cfg.APIKey == "" {
		return nil, nil, nil
	}
	return graphkit.New(cfg.APIURL, cfg.APIKey), nil, nil
}
