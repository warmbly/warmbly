package delegation

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// PurchasedConsentStatePrefix marks a purchased mailbox's Microsoft 365 tenant consent, which runs with no Warmbly session.
const PurchasedConsentStatePrefix = "mpc_"

// PurchasedConsentTTL is how long a tenant consent link stays usable.
const PurchasedConsentTTL = time.Hour

// Where a tenant consent ends, in the status its return URL carries.
const (
	ConsentConnected = "connected"
	ConsentCancelled = "cancelled"
	ConsentError     = "error"
)

// IsPurchasedConsentState reports whether state belongs to a purchased mailbox's tenant consent.
func IsPurchasedConsentState(state string) bool {
	return strings.HasPrefix(state, PurchasedConsentStatePrefix)
}

// TenantConsentMessage is what the owner of a mailbox waiting on its tenant's consent reads.
func TenantConsentMessage(domain string) string {
	return "Approve Warmbly in Microsoft 365 for " + domain + " with the domain's admin mailbox."
}

func errTenantConsent(domain string) *errx.Error {
	return errx.NewWithIdentifier(errx.Unprocessable, ErrIDMicrosoftTenantConsent, TenantConsentMessage(domain))
}

// MicrosoftConsent is a tenant consent link, or Consented when the workspace's grant for the tenant already works.
type MicrosoftConsent struct {
	URL       string
	ExpiresAt time.Time
	Consented bool
}

// ConsentWaker runs the connects of a workspace waiting on these domains' tenant consent now.
type ConsentWaker func(ctx context.Context, orgID uuid.UUID, domains []string)

// WireConsentWaker adds a waker told whenever a workspace's tenant consent is verified.
func (s *Service) WireConsentWaker(w ConsentWaker) { s.wakers = append(s.wakers, w) }

func (s *Service) wake(ctx context.Context, orgID uuid.UUID, domains []string) {
	for _, w := range s.wakers {
		w(ctx, orgID, domains)
	}
}

// microsoftConsentCodes are the token endpoint's answers for an app the tenant has not consented.
func microsoftConsentCodes(body string) bool {
	for _, c := range []string{"AADSTS700016", "AADSTS65001", "AADSTS7000229", "AADSTS500011"} {
		if strings.Contains(body, c) {
			return true
		}
	}
	return false
}

// microsoftConsentMissing tells a tenant that has not consented apart from any other failure.
func microsoftConsentMissing(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status == http.StatusUnauthorized || ae.status == http.StatusForbidden
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		return microsoftConsentCodes(re.ErrorCode + " " + re.ErrorDescription + " " + string(re.Body))
	}
	return false
}

// microsoftProbe is one application call into the tenant: nil when the app's consent works there.
func (s *Service) microsoftProbe(ctx context.Context, tenant string) error {
	return s.getJSON(ctx, s.microsoftSource(tenant), s.graphBase+"/users?$top=1&$select=id", nil)
}

// tenantGrant is the workspace's active Microsoft grant on tenant covering domain, nil when none.
func (s *Service) tenantGrant(ctx context.Context, orgID uuid.UUID, tenant, domain string) (*models.DomainGrant, error) {
	list, err := s.repo.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		g := &list[i]
		if g.Provider == models.GrantProviderMicrosoft && g.Status == "active" && strings.EqualFold(g.Tenant, tenant) && slices.Contains(g.Domains, domain) {
			return g, nil
		}
	}
	return nil, nil
}

func cleanConsentDomain(domain string) (string, bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || len(domain) > 253 || !strings.Contains(domain, ".") || strings.ContainsAny(domain, "@/:?# ") {
		return "", false
	}
	return domain, true
}

// StartPurchasedMicrosoftConsent opens this app's admin consent in a purchased domain's tenant, ending at returnURL;
// the administrator's sign-in after it is what binds the tenant to the workspace, recorded as made by userID.
func (s *Service) StartPurchasedMicrosoftConsent(ctx context.Context, orgID, userID uuid.UUID, domain, returnURL string) (*MicrosoftConsent, *errx.Error) {
	if !s.microsoftEnabled() {
		return nil, notConfigured("Microsoft 365")
	}
	if s.states == nil {
		return nil, errx.InternalError()
	}
	domain, ok := cleanConsentDomain(domain)
	if !ok {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNotCovered, "This is not a domain name.")
	}
	tenant, err := s.microsoftTenant(ctx, domain)
	if err != nil {
		return nil, unavailableErr()
	}
	g, err := s.tenantGrant(ctx, orgID, tenant, domain)
	if err != nil {
		return nil, errx.InternalError()
	}
	if g != nil && s.microsoftProbe(ctx, tenant) == nil {
		s.wake(ctx, orgID, g.Domains)
		return &MicrosoftConsent{Consented: true}, nil
	}
	state, err := newState(PurchasedConsentStatePrefix)
	if err != nil {
		return nil, errx.InternalError()
	}
	st := ConsentState{OrgID: orgID, UserID: userID, Domain: domain, Tenant: tenant, ReturnURL: returnURL}
	if err := s.states.Put(ctx, "mailbox_grant_state:"+state, st, PurchasedConsentTTL); err != nil {
		return nil, errx.InternalError()
	}
	q := url.Values{}
	q.Set("client_id", s.msClientID)
	q.Set("scope", "https://graph.microsoft.com/.default")
	q.Set("redirect_uri", s.msRedirect)
	q.Set("state", state)
	return &MicrosoftConsent{URL: s.msLoginBase + "/organizations/v2.0/adminconsent?" + q.Encode(), ExpiresAt: time.Now().Add(PurchasedConsentTTL).UTC()}, nil
}

// ConsentReturn is what Microsoft sent back to the callback, none of it trusted on its own.
type ConsentReturn struct {
	AdminConsent string
	Tenant       string
	Code         string
	Error        string
}

// FinishPurchasedMicrosoftConsent is where one return from Microsoft goes next: the administrator's sign-in, then the
// stored return URL with the outcome. ok is false for an unknown, used or expired state, which redirects nowhere.
func (s *Service) FinishPurchasedMicrosoftConsent(ctx context.Context, state string, in ConsentReturn) (string, bool) {
	if !IsPurchasedConsentState(state) || s.states == nil {
		return "", false
	}
	st, ok := s.states.Take(ctx, "mailbox_grant_state:"+state)
	if !ok || st.ReturnURL == "" || st.Domain == "" || st.Tenant == "" {
		return "", false
	}
	if in.Error != "" {
		if in.Error == "access_denied" || in.Error == "consent_required" {
			return consentReturnURL(st, ConsentCancelled)
		}
		return consentReturnURL(st, ConsentError)
	}
	if !s.microsoftEnabled() {
		return consentReturnURL(st, ConsentError)
	}
	if st.Verifier == "" {
		return s.chainConsentSignin(ctx, st, in)
	}
	// Past here the administrator may close the window; a consent that went through still lands.
	vctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	if xerr := s.recordTenantConsent(vctx, st, in.Code); xerr != nil {
		log.Info().Str("org_id", st.OrgID.String()).Str("reason", xerr.ResponseCode()).Msg("delegation: a tenant consent was not recorded")
		return consentReturnURL(st, ConsentError)
	}
	return consentReturnURL(st, ConsentConnected)
}

// chainConsentSignin follows a reported consent with the administrator's sign-in, under a new single-use state.
func (s *Service) chainConsentSignin(ctx context.Context, st ConsentState, in ConsentReturn) (string, bool) {
	if !strings.EqualFold(in.AdminConsent, "true") || !strings.EqualFold(strings.TrimSpace(in.Tenant), st.Tenant) {
		return consentReturnURL(st, ConsentError)
	}
	next, err := newState(PurchasedConsentStatePrefix)
	if err != nil {
		return consentReturnURL(st, ConsentError)
	}
	signin, err := newSigninState(st.OrgID, st.UserID)
	if err != nil {
		return consentReturnURL(st, ConsentError)
	}
	signin.Domain, signin.Tenant, signin.ReturnURL = st.Domain, st.Tenant, st.ReturnURL
	if err := s.states.Put(ctx, "mailbox_grant_state:"+next, signin, stateTTL); err != nil {
		return consentReturnURL(st, ConsentError)
	}
	return s.microsoftSigninConfig().AuthCodeURL(next, signinOptions(signin, oauth2.SetAuthURLParam("domain_hint", st.Domain))...), true
}

// recordTenantConsent proves the tenant's administrator signed in, then records the consent like any other.
func (s *Service) recordTenantConsent(ctx context.Context, st ConsentState, code string) *errx.Error {
	tenant, xerr := s.microsoftAdminTenant(ctx, code, st)
	if xerr != nil {
		return xerr
	}
	if !strings.EqualFold(tenant, st.Tenant) {
		return errx.NewWithIdentifier(errx.Forbidden, ErrIDProof, "The administrator signed in to another organization than the one "+st.Domain+" belongs to.")
	}
	return s.finishTenantConsent(ctx, st.OrgID, st.UserID, st.Domain, st.Tenant)
}

// finishTenantConsent follows an administrator's consent in tenant: it waits for the app to work there, records the
// workspace's grant for the tenant and wakes the connects waiting on it. Both the callback and the automatic consent end here.
func (s *Service) finishTenantConsent(ctx context.Context, orgID, userID uuid.UUID, domain, tenant string) *errx.Error {
	if xerr := s.awaitTenantConsent(ctx, tenant); xerr != nil {
		return xerr
	}
	g, xerr := s.verifyMicrosoft(ctx, tenant)
	if xerr != nil {
		return xerr
	}
	domains := map[string]bool{domain: true}
	for _, d := range g.Domains {
		domains[d] = true
	}
	g.Purchased = true
	if list, err := s.repo.List(ctx, orgID); err == nil {
		for _, prev := range list {
			if prev.Provider == models.GrantProviderMicrosoft && strings.EqualFold(prev.Tenant, tenant) {
				g.Purchased = prev.Purchased
				for _, d := range prev.Domains {
					domains[d] = true
				}
			}
		}
	}
	g.Domains = sortedKeys(domains)
	g.ID, g.OrganizationID = uuid.New(), orgID
	if err := s.repo.Upsert(ctx, g, userID); err != nil {
		return errx.InternalError()
	}
	s.wake(ctx, orgID, g.Domains)
	return nil
}

// awaitTenantConsent gives a fresh consent a short while to reach the token endpoint.
func (s *Service) awaitTenantConsent(ctx context.Context, tenant string) *errx.Error {
	var err error
	for attempt := 0; attempt < max(1, s.consentAttempts); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return unavailableErr()
			case <-time.After(s.consentPause):
			}
		}
		s.forget("m\x00" + tenant)
		if err = s.microsoftProbe(ctx, tenant); err == nil {
			return nil
		}
		if !microsoftConsentMissing(err) && !unavailable(err) {
			break
		}
	}
	return s.microsoftErr(err)
}

// consentReturnURL is the stored return URL with the domain and the outcome; only a stored URL is ever redirected to.
func consentReturnURL(st ConsentState, status string) (string, bool) {
	u, err := url.Parse(st.ReturnURL)
	if err != nil || !u.IsAbs() {
		return "", false
	}
	q := u.Query()
	q.Set("domain", st.Domain)
	q.Set("status", status)
	u.RawQuery = q.Encode()
	return u.String(), true
}
