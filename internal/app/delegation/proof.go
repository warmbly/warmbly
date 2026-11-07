package delegation

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
)

// A grant is only recorded for the workspace that proves it controls the
// domain or tenant: the instance's service account and Microsoft app are
// shared by every workspace, so an administrator authorizing them proves
// nothing about which workspace asked.
const (
	googleStatePrefix    = GoogleStatePrefix
	microsoftStatePrefix = MicrosoftStatePrefix
	stateTTL             = 15 * time.Minute
)

// ConsentState binds a sign-in round trip to who started it and what for.
type ConsentState struct {
	OrgID  uuid.UUID `json:"org_id"`
	UserID uuid.UUID `json:"user_id"`
	Domain string    `json:"domain,omitempty"`
	Admin  string    `json:"admin,omitempty"`
	// Verifier is the sign-in's PKCE verifier and Nonce the ID token must echo; neither leaves the server.
	Verifier     string `json:"verifier,omitempty"`
	Nonce        string `json:"nonce,omitempty"`
	ReturnOrigin string `json:"return_origin,omitempty"`
}

func (s *Service) OAuthReturnOrigin(ctx context.Context, state string) string {
	if s.states == nil || (!strings.HasPrefix(state, googleStatePrefix) && !strings.HasPrefix(state, microsoftStatePrefix)) {
		return ""
	}
	st, ok := s.states.Peek(ctx, "mailbox_grant_state:"+state)
	if !ok {
		return ""
	}
	if st.ReturnOrigin == "" {
		return config.PrimaryDashboardOrigin()
	}
	return config.DashboardOrigin(st.ReturnOrigin)
}

// TXTLookup reads a domain's TXT records, for the DNS proof.
type TXTLookup interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

func newState(prefix string) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// newSigninState is a consent state carrying a fresh PKCE verifier and OIDC nonce.
func newSigninState(orgID, userID uuid.UUID) (ConsentState, error) {
	nonce, err := newState("")
	if err != nil {
		return ConsentState{}, err
	}
	return ConsentState{OrgID: orgID, UserID: userID, Verifier: oauth2.GenerateVerifier(), Nonce: nonce}, nil
}

// signinOptions adds the S256 challenge and nonce of a consent state to an authorization URL.
func signinOptions(st ConsentState, extra ...oauth2.AuthCodeOption) []oauth2.AuthCodeOption {
	return append(extra, oauth2.S256ChallengeOption(st.Verifier), oauth2.SetAuthURLParam("nonce", st.Nonce))
}

// nonceMatches holds an ID token to the nonce its sign-in was started with.
func nonceMatches(claims map[string]any, st ConsentState) bool {
	return st.Nonce != "" && subtle.ConstantTimeCompare([]byte(claimString(claims, "nonce")), []byte(st.Nonce)) == 1
}

func (s *Service) takeState(ctx context.Context, prefix, state string, orgID, userID uuid.UUID) (ConsentState, *errx.Error) {
	refused := errx.NewWithIdentifier(errx.BadRequest, ErrIDState, "This sign-in expired or belongs to someone else. Start again.")
	if !strings.HasPrefix(state, prefix) || s.states == nil {
		return ConsentState{}, refused
	}
	st, ok := s.states.Take(ctx, "mailbox_grant_state:"+state)
	if !ok || st.OrgID != orgID || st.UserID != userID {
		return ConsentState{}, refused
	}
	return st, nil
}

// idTokenClaims reads the claims of an ID token this service received
// directly from the provider's token endpoint over TLS, in exchange for a code
// and its own client secret. Nothing else can put a token there, so the
// signature is not checked a second time.
func idTokenClaims(tok *oauth2.Token) (map[string]any, bool) {
	raw, _ := tok.Extra("id_token").(string)
	return jwtClaims(raw)
}

// jwtClaims reads a token the provider handed us directly over TLS; it verifies no signature.
func jwtClaims(raw string) (map[string]any, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil, false
	}
	return claims, true
}

func claimString(c map[string]any, k string) string {
	v, _ := c[k].(string)
	return v
}

// ---- Google -------------------------------------------------------------

// GoogleStatePrefix marks the state of an administrator's Google sign-in, so
// the sign-in callback can hand it to the dashboard instead of logging in.
const GoogleStatePrefix = "gac_"

// MicrosoftStatePrefix marks a Microsoft 365 admin consent, which the callback chains into a sign-in.
const MicrosoftStatePrefix = "mac_"

// GoogleStart is how a workspace proves it controls a Workspace domain.
type GoogleStart struct {
	// Method is "signin" (the administrator signs in with Google) when this
	// instance has a Google sign-in client, else "dns".
	Method string `json:"method"`
	URL    string `json:"url,omitempty"`
	State  string `json:"state,omitempty"`
	// The DNS proof always works, with or without a sign-in client.
	TXTName  string `json:"txt_name"`
	TXTValue string `json:"txt_value"`
}

func (s *Service) googleSigninEnabled() bool {
	return s.googleSignin != nil && s.googleSignin.ClientID != "" && s.googleSignin.ClientSecret != ""
}

func cleanDomainAdmin(domain, admin string) (string, string, *errx.Error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	admin = strings.ToLower(strings.TrimSpace(admin))
	if domain == "" || !strings.HasSuffix(admin, "@"+domain) || strings.Count(admin, "@") != 1 {
		return "", "", errx.NewWithIdentifier(errx.BadRequest, ErrIDNotCovered, "Enter the Workspace domain and an administrator address on that domain.")
	}
	return domain, admin, nil
}

// googleRetired refuses a Google grant for a domain the workspace holds no grant for; an existing or imported one may be proved again.
func (s *Service) googleRetired(ctx context.Context, orgID uuid.UUID, domain string) *errx.Error {
	list, err := s.repo.List(ctx, orgID)
	if err != nil {
		return errx.InternalError()
	}
	for _, g := range list {
		if g.Provider == models.GrantProviderGoogle && slices.Contains(g.Domains, domain) {
			return nil
		}
	}
	// Mailboxes that arrived in a workspace import without their grant are restored by making it again.
	awaiting, err := s.repo.AwaitsGoogleGrant(ctx, orgID, domain)
	if err != nil {
		return errx.InternalError()
	}
	if awaiting {
		return nil
	}
	return errx.NewWithIdentifier(errx.Forbidden, ErrIDGoogleRetired,
		"New Google Workspace domain-wide delegation grants are no longer set up. Have a Workspace administrator mark Warmbly as a trusted app, then sign in each mailbox with Google, or connect it with an app password. Grants already made keep working. See https://docs.warmbly.com/guides/mailboxes/#google-workspace-with-google-sign-in")
}

// StartGoogle begins the proof for a Workspace domain the workspace already holds a grant for.
func (s *Service) StartGoogle(ctx context.Context, orgID, userID uuid.UUID, domain, admin string) (*GoogleStart, *errx.Error) {
	if !s.googleEnabled() {
		return nil, notConfigured("Google Workspace")
	}
	domain, admin, xerr := cleanDomainAdmin(domain, admin)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.googleRetired(ctx, orgID, domain); xerr != nil {
		return nil, xerr
	}
	out := &GoogleStart{Method: "dns", TXTName: domainproof.Name(domain), TXTValue: s.proof.Value(orgID, domain)}
	if !s.googleSigninEnabled() || s.states == nil {
		return out, nil
	}
	state, err := newState(googleStatePrefix)
	if err != nil {
		return nil, errx.InternalError()
	}
	st, err := newSigninState(orgID, userID)
	if err != nil {
		return nil, errx.InternalError()
	}
	st.Domain, st.Admin = domain, admin
	st.ReturnOrigin = config.DashboardOriginFromContext(ctx)
	if err := s.states.Put(ctx, "mailbox_grant_state:"+state, st, stateTTL); err != nil {
		return nil, errx.InternalError()
	}
	out.Method, out.State = "signin", state
	out.URL = s.googleSignin.AuthCodeURL(state, signinOptions(st,
		oauth2.SetAuthURLParam("hd", domain),
		oauth2.SetAuthURLParam("login_hint", admin),
		oauth2.SetAuthURLParam("prompt", "select_account"))...)
	return out, nil
}

// GoogleFinish completes the proof: a sign-in code with its state, or the DNS record for domain and admin.
type GoogleFinish struct {
	State      string `json:"state"`
	Code       string `json:"code"`
	Domain     string `json:"domain"`
	AdminEmail string `json:"admin_email"`
}

// FinishGoogle records the grant once the workspace has proved the domain
// and the administrator is one in the directory.
func (s *Service) FinishGoogle(ctx context.Context, orgID, userID uuid.UUID, in GoogleFinish) (*models.DomainGrant, *errx.Error) {
	if !s.googleEnabled() {
		return nil, notConfigured("Google Workspace")
	}
	var domain, admin string
	if in.State != "" {
		st, xerr := s.takeState(ctx, googleStatePrefix, in.State, orgID, userID)
		if xerr != nil {
			return nil, xerr
		}
		domain, admin = st.Domain, st.Admin
		if !s.googleSigninEnabled() || strings.TrimSpace(in.Code) == "" || st.Verifier == "" {
			return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDState, "The Google sign-in did not complete. Start again.")
		}
		tok, err := s.googleSignin.Exchange(ctx, in.Code, oauth2.VerifierOption(st.Verifier))
		if err != nil {
			return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDState, "Google did not accept the sign-in. Start again.")
		}
		claims, ok := idTokenClaims(tok)
		if ok && !nonceMatches(claims, st) {
			return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDState, "Google did not accept the sign-in. Start again.")
		}
		verified, _ := claims["email_verified"].(bool)
		if !ok || !verified || !strings.EqualFold(claimString(claims, "email"), admin) || !strings.EqualFold(claimString(claims, "hd"), domain) {
			return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDProof, "Sign in as "+admin+", the administrator you entered, on "+domain+".")
		}
	} else {
		var xerr *errx.Error
		if domain, admin, xerr = cleanDomainAdmin(in.Domain, in.AdminEmail); xerr != nil {
			return nil, xerr
		}
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		txts, err := s.txt.LookupTXT(cctx, domainproof.Name(domain))
		cancel()
		var dnsErr *net.DNSError
		if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
			return nil, errx.NewWithIdentifier(errx.ServiceUnavailable, ErrIDUnavailable, "The DNS lookup did not complete. Try again in a minute.")
		}
		if !s.proof.Matches(txts, orgID, domain) {
			return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDProof,
				"The TXT record "+domainproof.Name(domain)+" with this workspace's value is not there yet. DNS changes can take a while to appear.")
		}
	}
	if xerr := s.googleRetired(ctx, orgID, domain); xerr != nil {
		return nil, xerr
	}
	g, xerr := s.verifyGoogle(ctx, domain, admin)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.googleRequireAdmin(ctx, admin); xerr != nil {
		return nil, xerr
	}
	g.ID, g.OrganizationID = uuid.New(), orgID
	if err := s.repo.Upsert(ctx, g, userID); err != nil {
		return nil, errx.InternalError()
	}
	return s.get(ctx, orgID, g.ID)
}

// googleRequireAdmin refuses an address the directory does not list as an administrator.
func (s *Service) googleRequireAdmin(ctx context.Context, admin string) *errx.Error {
	ts, err := s.googleSource(admin, s.directoryScope())
	if err != nil {
		return errx.InternalError()
	}
	var u struct {
		IsAdmin bool `json:"isAdmin"`
	}
	if err := s.getJSON(ctx, ts, s.adminBase+"/users/"+url.PathEscape(admin)+"?projection=basic", &u); err != nil {
		return s.googleErr(err)
	}
	if !u.IsAdmin {
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDProof, admin+" is not a super administrator of this Workspace.")
	}
	return nil
}

// ---- Microsoft ----------------------------------------------------------

// microsoftSigninConfig is the sign-in that follows the consent; its tokens
// name the tenant and the administrator's directory roles.
func (s *Service) microsoftSigninConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID: s.msClientID, ClientSecret: s.msSecret, RedirectURL: s.msRedirect,
		Scopes: []string{"openid", "https://graph.microsoft.com/User.Read"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  s.msLoginBase + "/organizations/oauth2/v2.0/authorize",
			TokenURL: s.msLoginBase + "/organizations/oauth2/v2.0/token",
		},
	}
}

// MicrosoftSigninURL is where the callback sends an administrator once
// Microsoft reports the consent; the sign-in carries the same state.
func (s *Service) MicrosoftSigninURL(ctx context.Context, state string) string {
	if !s.microsoftEnabled() || !strings.HasPrefix(state, microsoftStatePrefix) || s.states == nil {
		return ""
	}
	st, ok := s.states.Peek(ctx, "mailbox_grant_state:"+state)
	if !ok || st.Verifier == "" {
		return ""
	}
	return s.microsoftSigninConfig().AuthCodeURL(state, signinOptions(st)...)
}

// StartMicrosoft opens Microsoft's admin consent page, the only v2.0 route
// that grants application permissions; the callback chains the sign-in.
func (s *Service) StartMicrosoft(ctx context.Context, orgID, userID uuid.UUID) (string, string, *errx.Error) {
	if !s.microsoftEnabled() {
		return "", "", notConfigured("Microsoft 365")
	}
	if s.states == nil {
		return "", "", errx.InternalError()
	}
	state, err := newState(microsoftStatePrefix)
	if err != nil {
		return "", "", errx.InternalError()
	}
	st, err := newSigninState(orgID, userID)
	if err != nil {
		return "", "", errx.InternalError()
	}
	st.ReturnOrigin = config.DashboardOriginFromContext(ctx)
	if err := s.states.Put(ctx, "mailbox_grant_state:"+state, st, stateTTL); err != nil {
		return "", "", errx.InternalError()
	}
	q := url.Values{}
	q.Set("client_id", s.msClientID)
	q.Set("scope", "https://graph.microsoft.com/.default")
	q.Set("redirect_uri", s.msRedirect)
	q.Set("state", state)
	return s.msLoginBase + "/organizations/v2.0/adminconsent?" + q.Encode(), state, nil
}

// FinishMicrosoft records the tenant the administrator consented for. The
// tenant comes from the ID token this service redeems the code for, never
// from the caller.
func (s *Service) FinishMicrosoft(ctx context.Context, orgID, userID uuid.UUID, state, code string) (*models.DomainGrant, *errx.Error) {
	if !s.microsoftEnabled() {
		return nil, notConfigured("Microsoft 365")
	}
	st, xerr := s.takeState(ctx, microsoftStatePrefix, state, orgID, userID)
	if xerr != nil {
		return nil, xerr
	}
	if strings.TrimSpace(code) == "" || st.Verifier == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent, "Microsoft did not complete the consent. Start again as a Global Administrator.")
	}
	tok, err := s.microsoftSigninConfig().Exchange(context.WithValue(ctx, oauth2.HTTPClient, s.http), code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent, "Microsoft did not accept the consent. Start again as a Global Administrator.")
	}
	claims, ok := idTokenClaims(tok)
	if ok && !nonceMatches(claims, st) {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent, "Microsoft did not accept the consent. Start again as a Global Administrator.")
	}
	tenant := claimString(claims, "tid")
	if _, err := uuid.Parse(tenant); !ok || err != nil {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent, "Microsoft did not say which organization consented. Start again.")
	}
	// The consent alone proves nothing once another workspace has consented for
	// the tenant; the person signing in must hold a role that can grant it.
	// The ID token lists roles only when the app emits them; Graph's token does by default.
	access, _ := jwtClaims(tok.AccessToken)
	graphAdmin := strings.EqualFold(claimString(access, "tid"), tenant) && microsoftConsentAdmin(access)
	if !microsoftConsentAdmin(claims) && !graphAdmin {
		return nil, errx.NewWithIdentifier(errx.Forbidden, ErrIDProof,
			"Sign in as a Global Administrator or Privileged Role Administrator of the organization to connect it.")
	}
	s.forget("m\x00" + tenant)
	g, xerr := s.verifyMicrosoft(ctx, tenant)
	if xerr != nil {
		return nil, xerr
	}
	g.ID, g.OrganizationID = uuid.New(), orgID
	if err := s.repo.Upsert(ctx, g, userID); err != nil {
		return nil, errx.InternalError()
	}
	return s.get(ctx, orgID, g.ID)
}

// Directory roles that can grant tenant-wide consent to Graph application permissions.
var microsoftAdminRoles = map[string]bool{
	"62e90394-69f5-4237-9190-012177145e10": true, // Global Administrator
	"e8611ab8-c189-46e8-94e1-60213ab1f814": true, // Privileged Role Administrator
}

// microsoftConsentAdmin reports whether the ID token's wids claim holds an administrator role.
func microsoftConsentAdmin(claims map[string]any) bool {
	wids, _ := claims["wids"].([]any)
	for _, w := range wids {
		if id, _ := w.(string); microsoftAdminRoles[strings.ToLower(id)] {
			return true
		}
	}
	return false
}
