package delegation

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// PurchasedGoogleScopes are what a brokered token for a purchased Google
// mailbox may carry: the Gmail API scopes a delegated mailbox runs on, plus
// IMAP's, which is minted on its own.
var PurchasedGoogleScopes = []string{gmail.GmailModifyScope, gmail.GmailSettingsBasicScope, gmail.MailGoogleComScope}

// PurchasedTokens brokers Google tokens for mailboxes bought in the store,
// where the Workspace authorized the store's broker rather than this
// instance's service account. Implemented by the store.
type PurchasedTokens interface {
	GoogleToken(ctx context.Context, orgID uuid.UUID, email string, scopes []string) (*oauth2.Token, error)
	// Simulated reports the development simulator, whose tokens Google would refuse.
	Simulated() bool
}

// WirePurchasedTokens attaches the store's token broker.
func (s *Service) WirePurchasedTokens(p PurchasedTokens) { s.purchased = p }

// GoogleAppIdentity is this instance's service account for domain-wide
// delegation, which the store asks GraphKit to authorize in purchased
// Workspaces with every scope a delegated mailbox and its grant check use.
func (s *Service) GoogleAppIdentity() (clientID string, scopes []string, ok bool) {
	if !s.googleEnabled() || s.googleClientID == "" {
		return "", nil, false
	}
	return s.googleClientID, purchasedDelegationScopes(), true
}

func purchasedDelegationScopes() []string {
	out := append([]string(nil), config.GoogleDelegationScopes...)
	for _, sc := range PurchasedGoogleScopes {
		if !slices.Contains(out, sc) {
			out = append(out, sc)
		}
	}
	return out
}

// MicrosoftAppIdentity is the app and application permissions the store asks
// to be consented in purchased Microsoft 365 tenants; ok is false with no app.
func (s *Service) MicrosoftAppIdentity() (appID string, permissions []string, ok bool) {
	if !s.microsoftEnabled() {
		return "", nil, false
	}
	return s.msClientID, append([]string(nil), microsoftAppRoles...), true
}

type purchasedSource struct {
	s      *Service
	org    uuid.UUID
	email  string
	scopes []string
}

func (p purchasedSource) Token() (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return p.s.purchased.GoogleToken(ctx, p.org, p.email, p.scopes)
}

// purchasedGoogleSource reuses a brokered token until five minutes before it expires.
func (s *Service) purchasedGoogleSource(orgID uuid.UUID, email string, scopes ...string) oauth2.TokenSource {
	key := "p\x00" + orgID.String() + "\x00" + email + "\x00" + strings.Join(scopes, " ")
	if ts, ok := s.sources.Load(key); ok {
		return ts.(oauth2.TokenSource)
	}
	ts := oauth2.ReuseTokenSourceWithExpiry(nil, purchasedSource{s: s, org: orgID, email: email, scopes: scopes}, 5*time.Minute)
	s.sources.Store(key, ts)
	return ts
}

// purchasedProfile proves a brokered token reaches the mailbox and returns the
// address Gmail knows it by. The development simulator's tokens are opaque, so
// there the address stands as given and Google is never called.
func (s *Service) purchasedProfile(ctx context.Context, orgID uuid.UUID, address string) (string, error) {
	if s.purchased.Simulated() {
		return address, nil
	}
	return s.gmailProfile(ctx, s.purchasedGoogleSource(orgID, address, gmail.GmailModifyScope, gmail.GmailSettingsBasicScope))
}

// provePurchased proves the broker's token opens exactly the named mailbox, never another one it can reach.
func (s *Service) provePurchased(ctx context.Context, orgID uuid.UUID, address string) *errx.Error {
	got, err := s.purchasedProfile(ctx, orgID, address)
	if err != nil {
		return s.googleErr(err)
	}
	if !strings.EqualFold(strings.TrimSpace(got), address) {
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDMailboxUnreachable, "The token issued for this mailbox opens a different mailbox.")
	}
	return nil
}

// ConnectPartnerDelegated connects a mailbox a partner attached by delegation. The domain must already be covered
// by an active grant of this workspace that can mint for it (a grant it proved, or the store recorded for a domain it
// bought), unless an operator vouched the partner controls the domain; partner input alone never records a grant.
func (s *Service) ConnectPartnerDelegated(ctx context.Context, orgID, userID uuid.UUID, provider, address, name string, vouched bool) (*models.Email, *errx.Error) {
	address = strings.ToLower(strings.TrimSpace(address))
	g, xerr := s.PartnerDelegationGrant(ctx, orgID, provider, address)
	if xerr != nil {
		return nil, xerr
	}
	if g != nil {
		return s.connect(ctx, orgID, userID.String(), g.ID, address, name, true)
	}
	if !vouched {
		return nil, errPartnerDomainUnproven
	}
	return s.ConnectPurchased(ctx, orgID, userID, provider, address, name, "", false)
}

var errPartnerDomainUnproven = errx.NewWithIdentifier(errx.Unprocessable, ErrIDProof,
	"The workspace holds no verified grant for this domain. Verify the domain in the workspace, or ask the operator to allow it for delegation.")

// PartnerDelegationGrant is the workspace's active grant that can mint for the address without a broker, nil when none.
func (s *Service) PartnerDelegationGrant(ctx context.Context, orgID uuid.UUID, provider, address string) (*models.DomainGrant, *errx.Error) {
	at := strings.LastIndex(address, "@")
	if at <= 0 {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNotCovered, "This is not a mailbox address.")
	}
	switch provider {
	case models.GrantProviderGoogle:
		if !s.googleEnabled() {
			return nil, notConfigured("Google Workspace")
		}
	case models.GrantProviderMicrosoft:
		if !s.microsoftEnabled() {
			return nil, notConfigured("Microsoft 365")
		}
	default:
		return nil, errx.ErrNotFound
	}
	list, err := s.repo.List(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	for i := range list {
		g := &list[i]
		// A brokered Google grant mints only through its broker, so it says nothing about this instance's service account.
		if g.Provider == provider && g.Status == "active" && covers(g, address) && !(provider == models.GrantProviderGoogle && g.Purchased) {
			return g, nil
		}
	}
	return nil, nil
}

// gmailProfile proves a token from ts works and returns the address Gmail knows it by.
func (s *Service) gmailProfile(ctx context.Context, ts oauth2.TokenSource) (string, error) {
	var p struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := s.getJSON(ctx, ts, s.gmailBase+"/users/me/profile", &p); err != nil {
		return "", err
	}
	return p.EmailAddress, nil
}

// ConnectPurchased connects a mailbox bought in the store with no prompt. A
// brokered Google mailbox mints through the store's token broker under a grant
// marked purchased; an unbrokered one through this instance's own service
// account, which GraphKit authorized in the Workspace, under an ordinary grant
// read through admin, the domain's admin mailbox. A Microsoft 365 one goes
// through this instance's app once the tenant consented to it, and answers
// ErrIDMicrosoftTenantConsent until then (StartPurchasedMicrosoftConsent).
// It reuses the workspace's grant for the domain when one covers it.
func (s *Service) ConnectPurchased(ctx context.Context, orgID, userID uuid.UUID, provider, address, name, admin string, brokered bool) (*models.Email, *errx.Error) {
	address = strings.ToLower(strings.TrimSpace(address))
	at := strings.LastIndex(address, "@")
	if at <= 0 {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNotCovered, "This is not a mailbox address.")
	}
	domain := address[at+1:]
	list, err := s.repo.List(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	var g *models.DomainGrant
	switch provider {
	case models.GrantProviderGoogle:
		if brokered && s.purchased == nil {
			return nil, notConfigured("Google Workspace")
		}
		if !brokered && !s.googleEnabled() {
			return nil, notConfigured("Google Workspace")
		}
		for i := range list {
			if list[i].Provider == provider && list[i].Status == "active" && list[i].Purchased == brokered && slices.Contains(list[i].Domains, domain) {
				g = &list[i]
			}
		}
		if g == nil {
			subject := strings.ToLower(strings.TrimSpace(admin))
			if subject == "" {
				subject = address
			}
			if brokered {
				if xerr := s.provePurchased(ctx, orgID, address); xerr != nil {
					return nil, xerr
				}
			} else if _, err := s.googleProfile(ctx, subject); err != nil {
				return nil, s.googleErr(err)
			}
			g = &models.DomainGrant{ID: uuid.New(), OrganizationID: orgID, Provider: provider, Tenant: domain, AdminEmail: subject,
				Domains: []string{domain}, Purchased: brokered}
		}
	case models.GrantProviderMicrosoft:
		if !s.microsoftEnabled() {
			return nil, notConfigured("Microsoft 365")
		}
		tenant, err := s.microsoftTenant(ctx, domain)
		if err != nil {
			return nil, unavailableErr()
		}
		// A tenant that has not approved the app waits for its administrator, which the caller tells apart from a failure.
		if err := s.microsoftProbe(ctx, tenant); err != nil {
			if microsoftConsentMissing(err) {
				return nil, errTenantConsent(domain)
			}
			return nil, s.microsoftErr(err)
		}
		domains := []string{domain}
		for i := range list {
			if list[i].Provider == provider && list[i].Tenant == tenant {
				if list[i].Status == "active" && slices.Contains(list[i].Domains, domain) {
					g = &list[i]
				}
				for _, d := range list[i].Domains {
					if !slices.Contains(domains, d) {
						domains = append(domains, d)
					}
				}
			}
		}
		if g == nil {
			g = &models.DomainGrant{ID: uuid.New(), OrganizationID: orgID, Provider: provider, Tenant: tenant, Domains: domains, Purchased: true}
		}
	default:
		return nil, errx.ErrNotFound
	}
	if g.CreatedAt.IsZero() {
		if err := s.repo.Upsert(ctx, g, userID); err != nil {
			return nil, errx.InternalError()
		}
	}
	return s.connect(ctx, orgID, userID.String(), g.ID, address, name, true)
}
