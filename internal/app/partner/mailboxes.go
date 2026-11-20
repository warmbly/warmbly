package partner

import (
	"context"
	"errors"
	"math"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

// MailboxEmails is the slice of the email service the partner drives; every connect goes through it.
type MailboxEmails interface {
	OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)
	SetWarmupLifecycle(ctx context.Context, orgID, emailAccountID, action string) (*models.Email, *errx.Error)
	Update(ctx context.Context, orgID, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	Get(ctx context.Context, orgID, emailAccountID string) (*models.Email, *errx.Error)
	SyncWarmupPool(ctx context.Context, accountID uuid.UUID)
}

// AccountFinder finds a workspace's mailbox by address.
type AccountFinder interface {
	FindInOrganization(ctx context.Context, orgID uuid.UUID, email string) (*models.EmailRef, *errx.Error)
}

// PurchasedConnector connects a Google or Microsoft mailbox with no prompt (delegation.Service).
type PurchasedConnector interface {
	ConnectPurchased(ctx context.Context, orgID, userID uuid.UUID, provider, address, name, admin string, brokered bool) (*models.Email, *errx.Error)
	// ConnectPartnerDelegated connects under a grant the workspace already holds, or one an operator vouched for.
	ConnectPartnerDelegated(ctx context.Context, orgID, userID uuid.UUID, provider, address, name string, vouched bool) (*models.Email, *errx.Error)
	PartnerDelegationGrant(ctx context.Context, orgID uuid.UUID, provider, address string) (*models.DomainGrant, *errx.Error)
}

// WarmupScheduler seeds a mailbox's warmup chain once warmup is on.
type WarmupScheduler interface {
	EnsureWarmupScheduled(ctx context.Context, accountID uuid.UUID) error
}

// Credits adds money a partner was paid for to a workspace's balance, once per key.
type Credits interface {
	GrantPurchased(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, error)
}

// MailboxDeps wires the mailbox half of the partner API; Repo is required, the rest nil-safe where noted.
type MailboxDeps struct {
	Repo      repository.PartnerMailboxRepository
	Emails    MailboxEmails
	Accounts  AccountFinder
	Delegator PurchasedConnector
	Warmup    WarmupScheduler
	Credits   Credits
	Links     ConnectLinks
	// Consents opens Microsoft 365 tenant consent for mailboxes bought through the partner's infrastructure; nil turns the links off.
	Consents MicrosoftConsents
	Portals  PortalChecker
	// BrandsChanged drops cached brands after a group's changed.
	BrandsChanged func()
}

// WireMailboxes turns on the mailbox half: attach, enablement, connect links, usage and the group brand.
func (s *Service) WireMailboxes(d MailboxDeps) {
	s.mb = d
	s.enabled = s
}

func (s *Service) mailboxesOn() bool { return s != nil && s.mb.Repo != nil }

var (
	ErrMailboxNotFound   = errx.NewWithIdentifier(errx.NotFound, "not_found", "Mailbox not found.")
	ErrConnectionFailed  = errx.NewWithIdentifier(errx.Unprocessable, "connection_failed", "The mailbox could not be signed in to with these details.")
	ErrMailboxesDisabled = errx.NewPublic(errx.ServiceUnavailable, "Mailboxes are not available through the partner API on this instance.")
	// ErrDelegationUnproven refuses delegation for a domain neither verified in the workspace nor allowed by the operator.
	ErrDelegationUnproven = errx.NewWithIdentifier(errx.Unprocessable, "delegation_not_allowed",
		"Delegation is available for a domain the workspace verified, or one the operator allowed for this partner. Use the token method for a mailbox bought through the partner's infrastructure.")
)

const (
	connectBatch       = 10
	connectLease       = 3 * time.Minute
	connectMaxAttempts = 8
	connectFirstDelay  = 5 * time.Second
	connectFailedMsg   = "The mailbox could not be connected."
	maxPartnerMailbox  = 255
)

// MailboxWarmup is one mailbox's warmup as the partner reads it.
type MailboxWarmup struct {
	Enabled     bool       `json:"enabled"`
	State       string     `json:"state"`
	Score       *int       `json:"score"`
	Reason      *string    `json:"reason"`
	DailyTarget int        `json:"daily_target"`
	SentLast7d  int        `json:"sent_7d"`
	InboxRate7d *int       `json:"inbox_rate_7d"`
	StartedAt   *time.Time `json:"started_at"`
}

// Mailbox is the partner API's view of one mailbox.
type Mailbox struct {
	ID                      string  `json:"id"`
	Email                   string  `json:"email"`
	Provider                string  `json:"provider"`
	Source                  string  `json:"source"`
	PartnerMailboxID        *string `json:"partner_mailbox_id"`
	InfrastructureMailboxID *string `json:"infrastructure_mailbox_id"`
	Status                  string  `json:"status"`
	StatusError             *string `json:"status_error"`
	// ActionRequired names the step a person has to take before the mailbox connects (microsoft_consent), null when none.
	ActionRequired *string       `json:"action_required"`
	Enablement     string        `json:"enablement"`
	SendsToday     int           `json:"sends_today"`
	DailySendLimit int           `json:"daily_send_limit"`
	Warmup         MailboxWarmup `json:"warmup"`
	CreatedAt      time.Time     `json:"created_at"`
}

// mailboxView builds the answer for a row; limit is its workspace's daily cap.
func mailboxView(v *repository.PartnerMailboxView, limit int, now time.Time) Mailbox {
	m := v.Mailbox
	out := Mailbox{
		ID: m.ID.String(), Email: m.Email, Provider: m.Provider, Source: m.Source, PartnerMailboxID: m.PartnerMailboxID,
		Status: v.Status, Enablement: string(m.Enablement), SendsToday: v.CampaignSentToday, DailySendLimit: limit,
		CreatedAt: m.CreatedAt.UTC(),
	}
	if m.Source == models.PartnerSourceInfrastructure {
		out.InfrastructureMailboxID = m.PartnerMailboxID
	}
	if v.Status == "error" {
		msg := "The mailbox stopped connecting and needs to be connected again."
		switch {
		case m.ConnectError != nil && *m.ConnectError != "":
			msg = *m.ConnectError
		case v.AccountStatus != nil && *v.AccountStatus == "revoked":
			msg = "Access to the mailbox was revoked."
		}
		out.StatusError = &msg
	}
	if v.ActionRequired != nil {
		action := *v.ActionRequired
		out.ActionRequired = &action
		if m.ConnectError != nil && *m.ConnectError != "" {
			msg := *m.ConnectError
			out.StatusError = &msg
		}
	}
	out.Warmup = mailboxWarmup(v, now)
	return out
}

func mailboxWarmup(v *repository.PartnerMailboxView, now time.Time) MailboxWarmup {
	w := MailboxWarmup{State: v.WarmupState, Enabled: v.WarmupState != "off", SentLast7d: v.WarmupSent7d}
	if v.PlacementMajor >= models.PartnerScoreMinDeliveries {
		rate := int(math.Round(100 * float64(v.PlacementMajor-v.PlacementMajorSpam) / float64(v.PlacementMajor)))
		w.InboxRate7d, w.Score = &rate, &rate
	}
	switch models.WarmupHealthState(v.WarmupState) {
	case models.WarmupHealthWatch, models.WarmupHealthThrottled, models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
		w.Reason = v.HealthReason
	}
	if v.WarmupAt != nil {
		started := v.WarmupAt.UTC()
		w.StartedAt = &started
		if v.WarmupBase != nil && v.WarmupMax != nil && v.WarmupIncrease != nil {
			days := max(0, int(now.Sub(started).Hours()/24))
			w.DailyTarget = min(*v.WarmupMax, *v.WarmupBase+*v.WarmupIncrease*days)
		}
	}
	if !w.Enabled {
		w.DailyTarget = 0
	}
	return w
}

// publishedState is the fingerprint the change feed compares against.
func publishedState(v *repository.PartnerMailboxView) string {
	return repository.PartnerPublishedState(v.Status, v.Mailbox.Enablement, v.WarmupState, v.ActionRequired)
}

// AttachInput is the PUT /organizations/{external_id}/mailboxes/{partner_mailbox_id} body.
type AttachInput struct {
	PartnerMailboxID        string           `json:"partner_mailbox_id"`
	Source                  string           `json:"source"`
	InfrastructureMailboxID string           `json:"infrastructure_mailbox_id"`
	Email                   string           `json:"email"`
	Provider                string           `json:"provider"`
	FirstName               string           `json:"first_name"`
	LastName                string           `json:"last_name"`
	Enablement              string           `json:"enablement"`
	Connection              AttachConnection `json:"connection"`
}

// AttachConnection is how Warmbly signs in to the mailbox.
type AttachConnection struct {
	Method      string              `json:"method"`
	Credentials *MailboxCredentials `json:"credentials"`
}

// MailboxCredentials are IMAP/SMTP credentials for the password method; they are sealed on connect and never kept here.
type MailboxCredentials struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	AppPassword string   `json:"app_password"`
	IMAP        Endpoint `json:"imap"`
	SMTP        Endpoint `json:"smtp"`
}

// Endpoint is one mail server.
type Endpoint struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
}

// forget drops the credentials from memory once they are sealed.
func (c *MailboxCredentials) forget() {
	if c != nil {
		c.Password, c.AppPassword = "", ""
	}
}

// validateAttach checks a body against the contract and returns its normalized form.
func validateAttach(partnerMailboxID string, in *AttachInput) *errx.Error {
	partnerMailboxID = strings.TrimSpace(partnerMailboxID)
	if partnerMailboxID == "" || len(partnerMailboxID) > maxPartnerMailbox {
		return invalid("partner_mailbox_id is 1-255 characters.")
	}
	if in.PartnerMailboxID != "" && in.PartnerMailboxID != partnerMailboxID {
		return invalid("partner_mailbox_id in the body does not match the path.")
	}
	in.PartnerMailboxID = partnerMailboxID
	if in.Source == "" {
		in.Source = models.PartnerSourceInfrastructure
	}
	if in.Source != models.PartnerSourceInfrastructure && in.Source != models.PartnerSourcePartner {
		return invalid("source is graphkit_infrastructure or partner.")
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(in.Email))
	if err != nil || len(addr.Address) > 254 || !strings.Contains(addr.Address, "@") {
		return invalid("email is not a mailbox address.")
	}
	in.Email = strings.ToLower(addr.Address)
	switch in.Provider {
	case "google", "microsoft":
	case "smtp":
		if in.Source != models.PartnerSourcePartner {
			return invalid("provider smtp is for source partner.")
		}
	default:
		return invalid("provider is google, microsoft, or smtp for an outside mailbox.")
	}
	if in.Enablement == "" {
		in.Enablement = string(models.PartnerEnablementOff)
	}
	if !models.PartnerEnablement(in.Enablement).Valid() {
		return invalid("enablement is off, sending_warmup or warmup_only.")
	}
	switch in.Connection.Method {
	case "token":
		if in.Source != models.PartnerSourceInfrastructure || in.Provider != "google" {
			return invalid("connection.method token is for a Google mailbox bought through the partner's infrastructure.")
		}
	case "delegation":
		if in.Source != models.PartnerSourceInfrastructure || in.Provider == "smtp" {
			return invalid("connection.method delegation is for a Google or Microsoft mailbox bought through the partner's infrastructure.")
		}
	case "password":
		return validateCredentials(in)
	default:
		return invalid("connection.method is token, delegation or password.")
	}
	return nil
}

func validateCredentials(in *AttachInput) *errx.Error {
	c := in.Connection.Credentials
	if c == nil {
		return invalid("connection.credentials is required for the password method.")
	}
	secret := c.Password
	if in.Provider == "google" && c.AppPassword != "" {
		secret = c.AppPassword
	}
	if secret == "" || len(secret) > 1024 || len(c.Username) > 320 {
		return invalid("connection.credentials needs a password (an app_password for Google).")
	}
	for name, ep := range map[string]Endpoint{"imap": c.IMAP, "smtp": c.SMTP} {
		if strings.TrimSpace(ep.Host) == "" || len(ep.Host) > 253 || ep.Port < 1 || ep.Port > 65535 {
			return invalid("connection.credentials." + name + " needs a host and a port.")
		}
		switch strings.ToLower(ep.Security) {
		case "", "ssl", "tls", "starttls":
		default:
			return invalid("connection.credentials." + name + ".security is ssl or starttls.")
		}
	}
	return nil
}

func mailSecurity(s string) string {
	switch strings.ToLower(s) {
	case "ssl", "tls":
		return models.MailSecurityTLS
	case "starttls":
		return models.MailSecurityStartTLS
	}
	return ""
}

// pausedError is the refusal for turning something on in a workspace its partner paused.
func pausedError(po *models.PartnerOrganization) *errx.Error {
	if po.State == models.PartnerOrgCancelled {
		return ErrOrgCancelled
	}
	return ErrOrgSuspended
}

func (s *Service) senderName(first, last, email string) string {
	name := displayname.FullName(displayname.CleanOr(first, displayname.Person, ""), displayname.CleanOr(last, displayname.Person, ""))
	if name == "" {
		name = displayname.FromEmail(email)
	}
	return name
}

// Attach connects a mailbox the partner knows under its own id for it, or brings an attached one up to date.
func (s *Service) Attach(ctx context.Context, c *Caller, externalID, partnerMailboxID string, in AttachInput) (*Mailbox, bool, *errx.Error) {
	defer in.Connection.Credentials.forget()
	if !s.mailboxesOn() {
		return nil, false, ErrMailboxesDisabled
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, false, xerr
	}
	if xerr := validateAttach(partnerMailboxID, &in); xerr != nil {
		return nil, false, xerr
	}
	if po.State.Paused() {
		return nil, false, pausedError(po)
	}
	orgID := *po.OrganizationID
	method := in.Connection.Method
	existing, err := s.mb.Repo.GetPartnerMailboxByPartnerID(ctx, c.Partner.ID, c.Mode, po.ExternalID, in.PartnerMailboxID)
	if err != nil {
		return nil, false, internal(err)
	}
	created := existing == nil
	var row *models.PartnerMailbox
	if existing != nil {
		row = &existing.Mailbox
		if xerr := s.reattach(ctx, c, po, row, &in); xerr != nil {
			return nil, false, xerr
		}
	} else {
		row = &models.PartnerMailbox{
			ID: uuid.New(), PartnerID: c.Partner.ID, Mode: c.Mode, OrgExternalID: po.ExternalID, OrganizationID: &orgID,
			PartnerMailboxID: &in.PartnerMailboxID, Source: in.Source, Email: in.Email, Provider: in.Provider,
			FirstName: displayname.CleanOr(in.FirstName, displayname.Person, ""), LastName: displayname.CleanOr(in.LastName, displayname.Person, ""),
			ConnectionMethod: &method, ConnectState: models.PartnerConnectConnected,
		}
		if xerr := s.attachNew(ctx, c, po, row, &in); xerr != nil {
			return nil, false, xerr
		}
	}
	if xerr := s.applyEnablement(ctx, po, row.ID, models.PartnerEnablement(in.Enablement)); xerr != nil {
		return nil, false, xerr
	}
	s.audit(ctx, c.Partner, orgID, models.AuditActionConnect, models.AuditEntityEmailAccount, map[string]string{
		"partner_mailbox_id": in.PartnerMailboxID, "enablement": in.Enablement,
	})
	v, xerr := s.mailboxByID(ctx, po, row.ID)
	return v, created, xerr
}

// attachNew connects a mailbox new to the partner: an address the workspace already has is adopted as it is.
func (s *Service) attachNew(ctx context.Context, c *Caller, po *models.PartnerOrganization, row *models.PartnerMailbox, in *AttachInput) *errx.Error {
	orgID := *po.OrganizationID
	if s.mb.Accounts != nil {
		ref, xerr := s.mb.Accounts.FindInOrganization(ctx, orgID, in.Email)
		if xerr != nil {
			return xerr
		}
		if ref != nil && ref.ID != uuid.Nil {
			return s.adopt(ctx, c, row, ref.ID)
		}
	}
	switch in.Connection.Method {
	case "password":
		acc, xerr := s.connectPassword(ctx, c, po, in)
		if xerr != nil {
			return xerr
		}
		row.EmailAccountID = &acc.ID
		return s.insertMailbox(ctx, row)
	default:
		if xerr := s.checkDelegation(ctx, c.Partner, orgID, in.Source, in.Connection.Method, in.Provider, in.Email); xerr != nil {
			return xerr
		}
		// Brokered and delegated mailboxes connect in the background: the partner records the attach first,
		// and its token broker answers only for a mailbox it has attached.
		next := s.now().Add(connectFirstDelay)
		row.ConnectState, row.NextConnectAt = models.PartnerConnectPending, &next
		if xerr := s.insertMailbox(ctx, row); xerr != nil {
			return xerr
		}
		return s.markConsentPending(ctx, c.Partner, orgID, row)
	}
}

// consentPending reports an infrastructure Microsoft 365 mailbox whose tenant has not approved this instance's app
// for the workspace: no grant of the workspace covers its domain, and the operator did not vouch for it.
func (s *Service) consentPending(ctx context.Context, p *models.Partner, orgID uuid.UUID, row *models.PartnerMailbox) (bool, *errx.Error) {
	method := ""
	if row.ConnectionMethod != nil {
		method = *row.ConnectionMethod
	}
	if !infrastructureMicrosoft(row.Source, row.Provider, method) || delegationVouched(p, row.Email) || s.mb.Delegator == nil {
		return false, nil
	}
	g, xerr := s.mb.Delegator.PartnerDelegationGrant(ctx, orgID, grantProvider(row.Provider), row.Email)
	if xerr != nil {
		return false, xerr
	}
	return g == nil, nil
}

// markConsentPending records the approval a just-attached mailbox waits on, so the attach answer already carries it.
func (s *Service) markConsentPending(ctx context.Context, p *models.Partner, orgID uuid.UUID, row *models.PartnerMailbox) *errx.Error {
	pending, xerr := s.consentPending(ctx, p, orgID, row)
	if xerr != nil || !pending {
		// A grant check that failed leaves the decision to the background connect.
		return nil
	}
	if err := s.mb.Repo.AwaitPartnerAction(ctx, row.ID, models.ActionMicrosoftConsent, delegation.TenantConsentMessage(emailDomain(row.Email)), *row.NextConnectAt); err != nil {
		return internal(err)
	}
	return nil
}

func emailDomain(address string) string { return address[strings.LastIndex(address, "@")+1:] }

func (s *Service) insertMailbox(ctx context.Context, row *models.PartnerMailbox) *errx.Error {
	err := s.mb.Repo.InsertPartnerMailbox(ctx, row)
	if errors.Is(err, repository.ErrPartnerMailboxTaken) {
		return errx.NewWithIdentifier(errx.Conflict, "mailbox_exists", "This mailbox is already attached under another id.")
	}
	if err != nil {
		return internal(err)
	}
	return nil
}

// adopt puts an existing mailbox under the partner's id, converting the row it was connected under inside Warmbly.
func (s *Service) adopt(ctx context.Context, c *Caller, row *models.PartnerMailbox, accountID uuid.UUID) *errx.Error {
	current, err := s.mb.Repo.PartnerMailboxForAccount(ctx, accountID)
	if err != nil {
		return internal(err)
	}
	if current == nil {
		row.EmailAccountID = &accountID
		return s.insertMailbox(ctx, row)
	}
	if current.Source != models.PartnerSourceManual {
		return errx.NewWithIdentifier(errx.Conflict, "mailbox_exists", "This mailbox is already attached under another id.")
	}
	current.PartnerMailboxID, current.Source, current.ConnectionMethod = row.PartnerMailboxID, row.Source, row.ConnectionMethod
	current.FirstName, current.LastName, current.Provider = row.FirstName, row.LastName, row.Provider
	if err := s.mb.Repo.SavePartnerMailbox(ctx, current); err != nil {
		return internal(err)
	}
	*row = *current
	return nil
}

// reattach brings an attached mailbox up to date: a detached one comes back, a failed connect is tried again.
func (s *Service) reattach(ctx context.Context, c *Caller, po *models.PartnerOrganization, row *models.PartnerMailbox, in *AttachInput) *errx.Error {
	orgID := *po.OrganizationID
	row.FirstName = displayname.CleanOr(in.FirstName, displayname.Person, row.FirstName)
	row.LastName = displayname.CleanOr(in.LastName, displayname.Person, row.LastName)
	wasDetached := row.DetachedAt != nil
	row.DetachedAt, row.RemovedAt = nil, nil
	needsConnect := row.EmailAccountID == nil || row.ConnectState == models.PartnerConnectFailed
	if row.EmailAccountID != nil && wasDetached && s.mb.Emails != nil {
		active := "active"
		if _, xerr := s.mb.Emails.Update(ctx, orgID.String(), c.Partner.ServiceUserID.String(), row.EmailAccountID.String(), &models.UpdateEmail{Status: &active}); xerr != nil {
			if xerr.Code != errx.NotFound {
				return xerr
			}
			row.EmailAccountID, needsConnect = nil, true
		}
	}
	if needsConnect {
		method := in.Connection.Method
		row.ConnectionMethod, row.ConnectError, row.ConnectAttempts = &method, nil, 0
		if method == "password" {
			acc, xerr := s.connectPassword(ctx, c, po, in)
			if xerr != nil {
				return xerr
			}
			row.EmailAccountID, row.ConnectState, row.NextConnectAt = &acc.ID, models.PartnerConnectConnected, nil
		} else {
			if xerr := s.checkDelegation(ctx, c.Partner, orgID, row.Source, method, row.Provider, row.Email); xerr != nil {
				return xerr
			}
			next := s.now().Add(connectFirstDelay)
			row.ConnectState, row.NextConnectAt = models.PartnerConnectPending, &next
		}
	}
	if err := s.mb.Repo.SavePartnerMailbox(ctx, row); err != nil {
		if errors.Is(err, repository.ErrPartnerMailboxTaken) {
			return errx.NewWithIdentifier(errx.Conflict, "mailbox_exists", "This mailbox is already attached under another id.")
		}
		return internal(err)
	}
	if needsConnect && row.ConnectState == models.PartnerConnectPending {
		if xerr := s.markConsentPending(ctx, c.Partner, orgID, row); xerr != nil {
			return xerr
		}
	}
	if row.EmailAccountID != nil && s.mb.Emails != nil {
		name := s.senderName(row.FirstName, row.LastName, row.Email)
		if _, xerr := s.mb.Emails.Update(ctx, orgID.String(), c.Partner.ServiceUserID.String(), row.EmailAccountID.String(), &models.UpdateEmail{Name: &name}); xerr != nil {
			log.Info().Str("partner_mailbox", row.ID.String()).Str("reason", xerr.ResponseCode()).Msg("partner: sender name not updated")
		}
	}
	return nil
}

// grantProvider is the grant provider for a partner mailbox's provider.
func grantProvider(provider string) string {
	if provider == "microsoft" {
		return models.GrantProviderMicrosoft
	}
	return models.GrantProviderGoogle
}

// infrastructureMicrosoft is a Microsoft 365 mailbox bought through the partner's infrastructure and delegated to this
// instance's own app, which its tenant's administrator approves through a consent link.
func infrastructureMicrosoft(source, provider, method string) bool {
	return source == models.PartnerSourceInfrastructure && provider == "microsoft" && method == "delegation"
}

// delegationVouched reports a domain the operator allowed the partner to delegate.
func delegationVouched(p *models.Partner, address string) bool {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(address[at+1:])
	for _, d := range p.DelegationDomains {
		if d == domain {
			return true
		}
	}
	return false
}

// checkDelegation refuses a delegation attach for a domain the workspace never proved and the operator never vouched for.
func (s *Service) checkDelegation(ctx context.Context, p *models.Partner, orgID uuid.UUID, source, method, provider, address string) *errx.Error {
	// An infrastructure Microsoft 365 mailbox waits for its tenant's consent instead (microsoftConsentPending).
	if method != "delegation" || delegationVouched(p, address) || infrastructureMicrosoft(source, provider, method) {
		return nil
	}
	if s.mb.Delegator == nil {
		return ErrMailboxesDisabled
	}
	g, xerr := s.mb.Delegator.PartnerDelegationGrant(ctx, orgID, grantProvider(provider), address)
	if xerr != nil {
		return xerr
	}
	if g == nil {
		return ErrDelegationUnproven
	}
	return nil
}

// connectPassword connects through the dashboard's own SMTP/IMAP path, which checks and seals the credentials.
func (s *Service) connectPassword(ctx context.Context, c *Caller, po *models.PartnerOrganization, in *AttachInput) (*models.Email, *errx.Error) {
	if s.mb.Emails == nil {
		return nil, ErrMailboxesDisabled
	}
	cr := in.Connection.Credentials
	orgID := *po.OrganizationID
	secret, auth, host := cr.Password, models.MailAuthPassword, ""
	switch in.Provider {
	case "google":
		if cr.AppPassword != "" {
			secret, auth = cr.AppPassword, models.MailAuthAppPassword
		}
		host = string(mailhost.GoogleWorkspace)
	case "microsoft":
		host = string(mailhost.Microsoft365)
	}
	username := strings.TrimSpace(cr.Username)
	if username == "" {
		username = in.Email
	}
	data := &models.NewSMTPIMAPAccount{
		Name: s.senderName(in.FirstName, in.LastName, in.Email), Email: in.Email, MailHost: host, AuthMethod: auth,
		Purchased: in.Source == models.PartnerSourceInfrastructure,
		SMTP:      &models.Service{Username: username, Password: secret, Host: strings.TrimSpace(cr.SMTP.Host), Port: cr.SMTP.Port, Security: mailSecurity(cr.SMTP.Security)},
		IMAP:      &models.Service{Username: username, Password: secret, Host: strings.TrimSpace(cr.IMAP.Host), Port: cr.IMAP.Port, Security: mailSecurity(cr.IMAP.Security)},
	}
	acc, xerr := s.mb.Emails.OnboardSMTPIMAP(ctx, c.Partner.ServiceUserID.String(), &orgID, data)
	data.SMTP.Password, data.IMAP.Password = "", ""
	if xerr != nil {
		log.Warn().Str("organization_id", orgID.String()).Str("reason", xerr.ResponseCode()).Msg("partner: connecting a mailbox with its password failed")
		if xerr.Code == errx.Internal || xerr.Code == errx.ServiceUnavailable {
			return nil, xerr
		}
		return nil, errx.NewWithIdentifier(errx.Unprocessable, "connection_failed", "The mailbox could not be signed in to with these details: "+xerr.Message)
	}
	return acc, nil
}

// applyEnablement records an enablement and brings warmup in line with it.
func (s *Service) applyEnablement(ctx context.Context, po *models.PartnerOrganization, id uuid.UUID, e models.PartnerEnablement) *errx.Error {
	if _, err := s.mb.Repo.SetPartnerEnablement(ctx, id, e, s.now().UTC()); err != nil {
		return internal(err)
	}
	v, err := s.mb.Repo.GetPartnerMailboxByID(ctx, id)
	if err != nil {
		return internal(err)
	}
	if v != nil && v.Mailbox.EmailAccountID != nil {
		s.syncWarmup(ctx, *po.OrganizationID, *v.Mailbox.EmailAccountID, e)
	}
	return nil
}

// syncWarmup starts or pauses a mailbox's warmup to match its enablement; the gates enforce it either way.
func (s *Service) syncWarmup(ctx context.Context, orgID, accountID uuid.UUID, e models.PartnerEnablement) {
	if s.mb.Emails == nil {
		return
	}
	acc, xerr := s.mb.Emails.Get(ctx, orgID.String(), accountID.String())
	if xerr != nil || acc == nil {
		return
	}
	action := ""
	switch {
	case e.Warmup() && acc.Warmup == nil:
		action = "start"
	case e.Warmup() && acc.WarmupPausedAt != nil:
		action = "resume"
	case !e.Warmup() && acc.IsWarmingActive():
		action = "pause"
	}
	if action != "" {
		if _, xerr := s.mb.Emails.SetWarmupLifecycle(ctx, orgID.String(), accountID.String(), action); xerr != nil {
			log.Info().Str("email_account_id", accountID.String()).Str("action", action).Str("reason", xerr.ResponseCode()).Msg("partner: warmup not changed")
		}
	}
	if e.Warmup() && s.mb.Warmup != nil {
		_ = s.mb.Warmup.EnsureWarmupScheduled(ctx, accountID)
	}
	s.mb.Emails.SyncWarmupPool(ctx, accountID)
}

// SetEnablement is PUT /organizations/{external_id}/enablements/{mailbox_id}.
func (s *Service) SetEnablement(ctx context.Context, c *Caller, externalID, mailboxID, enablement string) (*Mailbox, *errx.Error) {
	if !s.mailboxesOn() {
		return nil, ErrMailboxesDisabled
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	e := models.PartnerEnablement(enablement)
	if !e.Valid() {
		return nil, invalid("enablement is off, sending_warmup or warmup_only.")
	}
	id, err := uuid.Parse(mailboxID)
	if err != nil {
		return nil, ErrMailboxNotFound
	}
	v, err := s.mb.Repo.GetPartnerMailbox(ctx, c.Partner.ID, c.Mode, po.ExternalID, id)
	if err != nil {
		return nil, internal(err)
	}
	if v == nil || v.Mailbox.DetachedAt != nil || v.Mailbox.RemovedAt != nil {
		return nil, ErrMailboxNotFound
	}
	if e != models.PartnerEnablementOff && po.State.Paused() {
		return nil, pausedError(po)
	}
	if xerr := s.applyEnablement(ctx, po, id, e); xerr != nil {
		return nil, xerr
	}
	if v.Mailbox.Enablement != e {
		s.audit(ctx, c.Partner, *po.OrganizationID, models.AuditActionUpdate, models.AuditEntityEmailAccount, map[string]string{
			"partner_mailbox": id.String(), "enablement": string(e),
		})
	}
	return s.mailboxByID(ctx, po, id)
}

// Detach is DELETE /organizations/{external_id}/mailboxes/{partner_mailbox_id}: the mailbox stops, its history stays.
func (s *Service) Detach(ctx context.Context, c *Caller, externalID, partnerMailboxID string) *errx.Error {
	if !s.mailboxesOn() {
		return ErrMailboxesDisabled
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return xerr
	}
	v, err := s.mb.Repo.GetPartnerMailboxByPartnerID(ctx, c.Partner.ID, c.Mode, po.ExternalID, partnerMailboxID)
	if err != nil {
		return internal(err)
	}
	if v == nil {
		return ErrMailboxNotFound
	}
	if v.Mailbox.DetachedAt != nil {
		return nil
	}
	orgID := *po.OrganizationID
	if err := s.mb.Repo.DetachPartnerMailbox(ctx, v.Mailbox.ID, s.now().UTC()); err != nil {
		return internal(err)
	}
	if acc := v.Mailbox.EmailAccountID; acc != nil && s.mb.Emails != nil {
		s.syncWarmup(ctx, orgID, *acc, models.PartnerEnablementOff)
		inactive := "inactive"
		if _, xerr := s.mb.Emails.Update(ctx, orgID.String(), c.Partner.ServiceUserID.String(), acc.String(), &models.UpdateEmail{Status: &inactive}); xerr != nil && xerr.Code != errx.NotFound {
			return xerr
		}
	}
	s.audit(ctx, c.Partner, orgID, models.AuditActionDisconnect, models.AuditEntityEmailAccount, map[string]string{"partner_mailbox_id": partnerMailboxID})
	return nil
}

// ListMailboxes is GET /organizations/{external_id}/mailboxes, oldest first.
func (s *Service) ListMailboxes(ctx context.Context, c *Caller, externalID, cursor string, limit int) (*Page[Mailbox], *errx.Error) {
	if !s.mailboxesOn() {
		return nil, ErrMailboxesDisabled
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	var after *repository.PartnerMailboxCursor
	if cursor != "" {
		oc, ok := DecodeCursor(cursor)
		if !ok {
			return nil, invalid("cursor is not valid.")
		}
		after = &repository.PartnerMailboxCursor{CreatedAt: oc.CreatedAt, ID: oc.ID}
	}
	rows, err := s.mb.Repo.ListPartnerMailboxes(ctx, c.Partner.ID, c.Mode, po.ExternalID, after, limit+1)
	if err != nil {
		return nil, internal(err)
	}
	page := &Page[Mailbox]{Data: []Mailbox{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.Pagination.HasMore = true
		next := EncodeCursor(rows[limit-1].Mailbox.CreatedAt, rows[limit-1].Mailbox.ID)
		page.Pagination.NextCursor = &next
	}
	now := s.now()
	for i := range rows {
		page.Data = append(page.Data, mailboxView(&rows[i], po.CampaignSendsPerMailboxPerDay, now))
	}
	return page, nil
}

func (s *Service) mailboxByID(ctx context.Context, po *models.PartnerOrganization, id uuid.UUID) (*Mailbox, *errx.Error) {
	v, err := s.mb.Repo.GetPartnerMailboxByID(ctx, id)
	if err != nil {
		return nil, internal(err)
	}
	if v == nil {
		return nil, ErrMailboxNotFound
	}
	m := mailboxView(v, po.CampaignSendsPerMailboxPerDay, s.now())
	return &m, nil
}

// EnabledCounts implements EnabledCounter: a workspace's mailboxes per enablement, for usage.enabled.
func (s *Service) EnabledCounts(ctx context.Context, orgID uuid.UUID) (EnabledCounts, error) {
	sw, wo, err := s.mb.Repo.PartnerEnabledCounts(ctx, orgID)
	return EnabledCounts{SendingWarmup: sw, WarmupOnly: wo}, err
}

// MailboxWarmupHeld reports a mailbox its workspace's partner did not enable for warmup, or a test-mode workspace's.
// It satisfies the feature gate's per-mailbox hold; a workspace no partner manages is never held.
func (s *Service) MailboxWarmupHeld(ctx context.Context, orgID, mailboxID uuid.UUID) (bool, error) {
	if s == nil || !s.mailboxesOn() {
		return false, nil
	}
	ref, err := s.ManagedRef(ctx, orgID)
	if err != nil || ref == nil {
		return false, err
	}
	if ref.Test {
		return true, nil
	}
	row, err := s.mb.Repo.PartnerMailboxForAccount(ctx, mailboxID)
	if err != nil {
		return true, err
	}
	return row == nil || !row.Enablement.Warmup(), nil
}

// MailboxLock is the partner that holds a mailbox it attached to the workspace; members cannot remove or reconnect it while it does.
func (s *Service) MailboxLock(ctx context.Context, orgID, emailAccountID uuid.UUID) (string, error) {
	if s == nil || !s.mailboxesOn() {
		return "", nil
	}
	return s.mb.Repo.PartnerMailboxLock(ctx, orgID, emailAccountID)
}

// WorkspaceMailbox is one mailbox's enablement as a managed workspace's members see it.
type WorkspaceMailbox struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Enablement     string    `json:"enablement"`
	Managed        bool      `json:"managed"`
}

// WorkspaceMailboxes is the read-only enablement of every mailbox in a managed workspace, nil for any other.
func (s *Service) WorkspaceMailboxes(ctx context.Context, orgID uuid.UUID) ([]WorkspaceMailbox, *errx.Error) {
	out := []WorkspaceMailbox{}
	if s == nil || !s.mailboxesOn() {
		return out, nil
	}
	rows, err := s.mb.Repo.ListOrganizationPartnerMailboxes(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	for _, r := range rows {
		if r.EmailAccountID == nil {
			continue
		}
		out = append(out, WorkspaceMailbox{EmailAccountID: *r.EmailAccountID, Enablement: string(r.Enablement), Managed: r.Attached()})
	}
	return out, nil
}

// TurnOffAll sets every mailbox of a workspace off, as cancelling it does.
func (s *Service) turnOffAll(ctx context.Context, orgID uuid.UUID) {
	if !s.mailboxesOn() {
		return
	}
	ids, err := s.mb.Repo.TurnOffPartnerOrganization(ctx, orgID, s.now().UTC())
	if err != nil {
		errs.CaptureException(err)
		return
	}
	for _, id := range ids {
		v, err := s.mb.Repo.GetPartnerMailboxByID(ctx, id)
		if err == nil && v != nil && v.Mailbox.EmailAccountID != nil {
			s.syncWarmup(ctx, orgID, *v.Mailbox.EmailAccountID, models.PartnerEnablementOff)
		}
	}
}
