package partner

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/connectlink"
	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// ConnectLinkTTL is how long a partner's connect link stays usable.
const ConnectLinkTTL = time.Hour

// ConnectLinks mints the client connect links a partner's links ride on.
type ConnectLinks interface {
	CreateSingle(ctx context.Context, orgID, creator uuid.UUID, label string, provider models.InboxProvider, ttl time.Duration) (*models.CreatedClientConnectLink, *errx.Error)
}

// ConnectLinkInput is the POST /organizations/{external_id}/connect-links body.
type ConnectLinkInput struct {
	Provider         string `json:"provider"`
	PartnerMailboxID string `json:"partner_mailbox_id"`
	Enablement       string `json:"enablement"`
	LoginHint        string `json:"login_hint"`
	ReturnURL        string `json:"return_url"`
}

// ConnectLink is a one-time hosted connect URL.
type ConnectLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CleanReturnURL accepts an absolute HTTPS URL on one of the partner's allowed hosts (http in development).
func CleanReturnURL(raw string, allowedHosts []string) (string, bool) {
	clean, ok := CleanReturnTo(raw, allowedHosts)
	if !ok || clean == "" || strings.HasPrefix(clean, "/") {
		return "", false
	}
	return clean, true
}

// CreateConnectLink mints a single-use, hour-long link through which the person connects one Google or Microsoft mailbox.
func (s *Service) CreateConnectLink(ctx context.Context, c *Caller, externalID string, in ConnectLinkInput) (*ConnectLink, *errx.Error) {
	if !s.mailboxesOn() || s.mb.Links == nil {
		return nil, ErrMailboxesDisabled
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	var provider models.InboxProvider
	switch in.Provider {
	case "google":
		provider = models.InboxProviderGoogle
	case "microsoft":
		provider = models.InboxProviderOutlook
	default:
		return nil, invalid("provider is google or microsoft.")
	}
	pmID := strings.TrimSpace(in.PartnerMailboxID)
	if pmID == "" || len(pmID) > maxPartnerMailbox {
		return nil, invalid("partner_mailbox_id is 1-255 characters.")
	}
	e := models.PartnerEnablement(in.Enablement)
	if in.Enablement == "" {
		e = models.PartnerEnablementOff
	}
	if !e.Valid() {
		return nil, invalid("enablement is off, sending_warmup or warmup_only.")
	}
	hint := strings.TrimSpace(in.LoginHint)
	if hint != "" {
		if a, err := mail.ParseAddress(hint); err != nil || a.Address != hint || len(hint) > 254 {
			return nil, invalid("login_hint is a mailbox address.")
		}
	}
	back, ok := CleanReturnURL(in.ReturnURL, c.Partner.AllowedHosts)
	if !ok {
		return nil, invalid("return_url is not on a host configured for this partner.")
	}
	if po.State.Paused() {
		return nil, pausedError(po)
	}
	orgID := *po.OrganizationID
	created, xerr := s.mb.Links.CreateSingle(ctx, orgID, c.Partner.ServiceUserID, "Connect link from "+c.Partner.DisplayName, provider, ConnectLinkTTL)
	if xerr != nil {
		if xerr == connectlink.ErrProviderUnavailable {
			return nil, errx.NewWithIdentifier(errx.Unprocessable, "provider_unavailable", "Connecting this provider is not configured on this instance.")
		}
		if xerr == connectlink.ErrUnavailable {
			return nil, errx.NewWithIdentifier(errx.Unprocessable, "provider_unavailable", "Hosted connect pages are not configured on this instance.")
		}
		return nil, xerr
	}
	if err := s.mb.Repo.InsertPartnerConnectLink(ctx, &repository.PartnerConnectLink{
		LinkID: created.Link.ID, PartnerID: c.Partner.ID, Mode: c.Mode, OrganizationID: orgID, OrgExternalID: po.ExternalID,
		PartnerMailboxID: pmID, Provider: in.Provider, Enablement: e, LoginHint: hint, ReturnURL: back,
	}); err != nil {
		return nil, internal(err)
	}
	return &ConnectLink{URL: created.URL, ExpiresAt: created.Link.ExpiresAt.UTC()}, nil
}

// LoginHint implements connectlink.PartnerLinks.
func (s *Service) LoginHint(ctx context.Context, linkID uuid.UUID) string {
	l, err := s.mb.Repo.GetPartnerConnectLink(ctx, linkID)
	if err != nil || l == nil {
		return ""
	}
	return l.LoginHint
}

// ReturnURL implements connectlink.PartnerLinks: the partner's URL with the mailbox and the outcome.
func (s *Service) ReturnURL(ctx context.Context, linkID uuid.UUID, status string) (string, bool) {
	l, err := s.mb.Repo.GetPartnerConnectLink(ctx, linkID)
	if err != nil || l == nil {
		return "", false
	}
	u, err := url.Parse(l.ReturnURL)
	if err != nil {
		return "", false
	}
	q := u.Query()
	q.Set("partner_mailbox_id", l.PartnerMailboxID)
	q.Set("status", status)
	u.RawQuery = q.Encode()
	return u.String(), true
}

// Connected implements connectlink.PartnerLinks: the connected mailbox goes under the partner's id with the link's enablement.
func (s *Service) Connected(ctx context.Context, linkID uuid.UUID, acc *models.Email) {
	l, err := s.mb.Repo.GetPartnerConnectLink(ctx, linkID)
	if err != nil || l == nil || acc == nil {
		if err != nil {
			errs.CaptureException(err)
		}
		return
	}
	po, err := s.Repo.GetPartnerOrg(ctx, l.PartnerID, l.Mode, l.OrgExternalID)
	if err != nil || po == nil || po.OrganizationID == nil {
		return
	}
	method := "oauth"
	provider := "google"
	if l.Provider == "microsoft" {
		provider = "microsoft"
	}
	pmID := l.PartnerMailboxID
	row := &models.PartnerMailbox{ID: uuid.New(), PartnerID: l.PartnerID, Mode: l.Mode, OrgExternalID: l.OrgExternalID,
		OrganizationID: po.OrganizationID, PartnerMailboxID: &pmID, Source: models.PartnerSourcePartner, Email: strings.ToLower(acc.Email),
		Provider: provider, ConnectionMethod: &method, ConnectState: models.PartnerConnectConnected}
	existing, err := s.mb.Repo.GetPartnerMailboxByPartnerID(ctx, l.PartnerID, l.Mode, l.OrgExternalID, pmID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if existing != nil {
		row = &existing.Mailbox
		row.EmailAccountID, row.Email, row.ConnectionMethod = &acc.ID, strings.ToLower(acc.Email), &method
		row.ConnectState, row.ConnectError, row.DetachedAt, row.RemovedAt = models.PartnerConnectConnected, nil, nil, nil
		err = s.mb.Repo.SavePartnerMailbox(ctx, row)
	} else {
		var current *models.PartnerMailbox
		if current, err = s.mb.Repo.PartnerMailboxForAccount(ctx, acc.ID); err == nil && current != nil && current.Source == models.PartnerSourceManual {
			current.PartnerMailboxID, current.Source, current.ConnectionMethod, current.Provider = &pmID, models.PartnerSourcePartner, &method, provider
			row, err = current, s.mb.Repo.SavePartnerMailbox(ctx, current)
		} else if err == nil {
			row.EmailAccountID = &acc.ID
			err = s.mb.Repo.InsertPartnerMailbox(ctx, row)
		}
	}
	if err != nil {
		if !errors.Is(err, repository.ErrPartnerMailboxTaken) {
			errs.CaptureException(err)
		}
		log.Warn().Str("link_id", linkID.String()).Msg("partner: a connect link's mailbox was not recorded")
		return
	}
	if xerr := s.applyEnablement(ctx, po, row.ID, l.Enablement); xerr != nil {
		return
	}
	v, err := s.mb.Repo.GetPartnerMailboxByID(ctx, row.ID)
	if err != nil || v == nil {
		return
	}
	view := mailboxView(v, po.CampaignSendsPerMailboxPerDay, s.now())
	if err := s.Publish(ctx, l.PartnerID, l.Mode, models.PartnerEventMailboxCreated, l.OrgExternalID, view); err != nil {
		errs.CaptureException(err)
	}
	_ = s.mb.Repo.SetPartnerMailboxPublished(ctx, row.ID, publishedState(v))
}

// MicrosoftConsents opens a Microsoft 365 tenant's consent to this instance's app (delegation.Service).
type MicrosoftConsents interface {
	StartPurchasedMicrosoftConsent(ctx context.Context, orgID, userID uuid.UUID, domain, returnURL string) (*delegation.MicrosoftConsent, *errx.Error)
}

// MicrosoftConsentInput is the POST /organizations/{external_id}/microsoft-consent-links body.
type MicrosoftConsentInput struct {
	Domain    string `json:"domain"`
	ReturnURL string `json:"return_url"`
}

// MicrosoftConsentLink is a one-time consent URL, or url null and consented true when the workspace needs none.
type MicrosoftConsentLink struct {
	URL       *string    `json:"url"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Consented bool       `json:"consented,omitempty"`
}

// CreateMicrosoftConsentLink mints a single-use, hour-long link through which the tenant's administrator approves
// this instance's app for a domain of the workspace's Microsoft 365 mailboxes, and lands back on return_url.
func (s *Service) CreateMicrosoftConsentLink(ctx context.Context, c *Caller, externalID string, in MicrosoftConsentInput) (*MicrosoftConsentLink, *errx.Error) {
	if !s.mailboxesOn() || s.mb.Consents == nil {
		return nil, ErrMailboxesDisabled
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if domain == "" || len(domain) > 253 || !strings.Contains(domain, ".") || strings.ContainsAny(domain, "@/:?# ") {
		return nil, invalid("domain is a domain name.")
	}
	back, ok := CleanReturnURL(in.ReturnURL, c.Partner.AllowedHosts)
	if !ok {
		return nil, invalid("return_url is not on a host configured for this partner.")
	}
	if po.State.Paused() {
		return nil, pausedError(po)
	}
	attached, err := s.mb.Repo.PartnerMailboxOnDomain(ctx, c.Partner.ID, c.Mode, po.ExternalID, "microsoft", domain)
	if err != nil {
		return nil, internal(err)
	}
	if !attached {
		return nil, errx.NewWithIdentifier(errx.NotFound, "not_found", "No Microsoft 365 mailbox on this domain is attached to the organization.")
	}
	consent, xerr := s.mb.Consents.StartPurchasedMicrosoftConsent(ctx, *po.OrganizationID, c.Partner.ServiceUserID, domain, back)
	if xerr != nil {
		if xerr.Identifier == delegation.ErrIDNotConfigured {
			return nil, errx.NewWithIdentifier(errx.Unprocessable, "provider_unavailable", "Connecting Microsoft 365 is not configured on this instance.")
		}
		if xerr.Identifier == delegation.ErrIDUnavailable {
			return nil, &errx.Error{Code: errx.ServiceUnavailable, Identifier: "upstream_unavailable", Message: "Microsoft did not answer. Try again in a minute.", RetryAfter: 30}
		}
		return nil, xerr
	}
	if consent.Consented {
		return &MicrosoftConsentLink{Consented: true}, nil
	}
	u, at := consent.URL, consent.ExpiresAt.UTC()
	return &MicrosoftConsentLink{URL: &u, ExpiresAt: &at}, nil
}
