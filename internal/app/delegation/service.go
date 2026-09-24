// Package delegation connects whole domains of mailboxes through an
// administrator's grant: Google Workspace domain-wide delegation to the
// instance's service account, and Microsoft 365 application consent to the
// instance's Microsoft app. No token is stored; one is minted per use from the
// instance's own credentials, so revoking the grant stops everything at once.
package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

// Mailboxes is the mailbox service's side of a delegated connect.
type Mailboxes interface {
	ConnectDelegated(ctx context.Context, userID string, orgID *uuid.UUID, data models.NewDelegatedAccount) (*models.Email, *errx.Error)
	ReactivateDelegated(ctx context.Context, accountID uuid.UUID) (*models.Email, *errx.Error)
	Update(ctx context.Context, orgID, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
}

// Store is the mailbox store's side.
type Store interface {
	GetByID(ctx context.Context, emailAccountID uuid.UUID) (*models.Email, *errx.Error)
	GetDelegation(ctx context.Context, accountID uuid.UUID) (*models.DelegatedMailbox, *errx.Error)
	FindManyInOrganization(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]models.EmailRef, *errx.Error)
	ListSigninRetiring(ctx context.Context, orgID uuid.UUID) ([]models.MigrationMailbox, *errx.Error)
}

// Config is what the dashboard needs to walk an administrator through a grant.
type Config struct {
	GoogleEnabled    bool     `json:"google_enabled"`
	GoogleClientID   string   `json:"google_client_id,omitempty"`
	GoogleScopes     []string `json:"google_scopes"`
	MicrosoftEnabled bool     `json:"microsoft_enabled"`
	// GoogleMissing and MicrosoftMissing name the settings an operator still has to set; empty when enabled.
	GoogleMissing    []string `json:"google_missing"`
	MicrosoftMissing []string `json:"microsoft_missing"`
}

// Stable codes for refusals a client branches on.
const (
	ErrIDNotConfigured      = "mailbox_grant_not_configured"
	ErrIDGoogleUnauthorized = "google_delegation_unauthorized"
	ErrIDMicrosoftConsent   = "microsoft_consent_missing"
	ErrIDState              = "mailbox_grant_state_invalid"
	ErrIDNotCovered         = "mailbox_grant_domain_mismatch"
	ErrIDMailboxUnreachable = "mailbox_grant_mailbox_unreachable"
	ErrIDGrantInactive      = "mailbox_grant_inactive"
	ErrIDProof              = "mailbox_grant_proof_missing"
	ErrIDUnavailable        = "mailbox_grant_unavailable"
)

// States holds a consent round trip's state until the administrator returns.
type States interface {
	Put(ctx context.Context, key string, v ConsentState, ttl time.Duration) error
	// Take reads and removes; a second Take of the same key fails.
	Take(ctx context.Context, key string) (ConsentState, bool)
}

type redisStates struct{ c *cache.Cache }

func (r redisStates) Put(ctx context.Context, key string, v ConsentState, ttl time.Duration) error {
	return r.c.SetJSON(ctx, key, v, ttl)
}

func (r redisStates) Take(ctx context.Context, key string) (ConsentState, bool) {
	var v ConsentState
	if err := r.c.GetJSON(ctx, key, &v); err != nil {
		return v, false
	}
	if n, err := r.c.Del(ctx, key).Result(); err != nil || n == 0 {
		return v, false
	}
	return v, true
}

type Service struct {
	repo      repository.DomainGrantRepository
	mailboxes Mailboxes
	store     Store
	states    States
	http      *http.Client

	googleKey      []byte
	googleClientID string
	msClientID     string
	msSecret       string
	msRedirect     string
	// googleSignin is the instance's Google sign-in client (openid email), for the admin's proof.
	googleSignin *oauth2.Config
	proof        *domainproof.Prover
	txt          TXTLookup

	// Endpoints, overridable in tests.
	googleTokenURL string
	adminBase      string
	gmailBase      string
	msLoginBase    string
	graphBase      string

	sources sync.Map // cache key -> oauth2.TokenSource
}

// Deps builds a Service. MicrosoftRedirect is the registered Outlook OAuth callback.
type Deps struct {
	Repo      repository.DomainGrantRepository
	Mailboxes Mailboxes
	Store     Store
	Cache     *cache.Cache
	// States overrides Cache for the consent round trip (tests).
	States            States
	GoogleKey         []byte
	MicrosoftClientID string
	MicrosoftSecret   string
	MicrosoftRedirect string
	// GoogleSignin is optional; without it a Google grant is proved by DNS.
	GoogleSignin *oauth2.Config
	Prover       *domainproof.Prover
	TXT          TXTLookup
}

func NewService(d Deps) *Service {
	s := &Service{
		repo: d.Repo, mailboxes: d.Mailboxes, store: d.Store, states: d.States,
		http:       &http.Client{Timeout: 20 * time.Second},
		googleKey:  d.GoogleKey,
		msClientID: d.MicrosoftClientID, msSecret: d.MicrosoftSecret, msRedirect: d.MicrosoftRedirect,
		googleSignin: d.GoogleSignin, proof: d.Prover, txt: d.TXT,
		googleTokenURL: google.JWTTokenURL,
		adminBase:      "https://admin.googleapis.com/admin/directory/v1",
		gmailBase:      "https://gmail.googleapis.com/gmail/v1",
		msLoginBase:    "https://login.microsoftonline.com",
		graphBase:      "https://graph.microsoft.com/v1.0",
	}
	if s.txt == nil {
		s.txt = net.DefaultResolver
	}
	if s.proof == nil {
		s.proof = domainproof.New("")
	}
	if s.states == nil && d.Cache != nil {
		s.states = redisStates{c: d.Cache}
	}
	if len(d.GoogleKey) > 0 {
		var key struct {
			ClientID string `json:"client_id"`
			Type     string `json:"type"`
		}
		if err := json.Unmarshal(d.GoogleKey, &key); err != nil || key.Type != "service_account" || key.ClientID == "" {
			log.Error().Msg("delegation: GOOGLE_WORKSPACE_DELEGATION_KEY is not a service account key; Google Workspace grants are off")
			s.googleKey = nil
		} else {
			s.googleClientID = key.ClientID
		}
	}
	return s
}

func (s *Service) googleEnabled() bool    { return len(s.googleKey) > 0 }
func (s *Service) microsoftEnabled() bool { return s.msClientID != "" && s.msSecret != "" }

// Config says which grants this instance can take and what the Google administrator authorizes.
func (s *Service) Config() Config {
	c := Config{
		GoogleEnabled: s.googleEnabled(), GoogleClientID: s.googleClientID,
		GoogleScopes: config.GoogleDelegationScopes, MicrosoftEnabled: s.microsoftEnabled(),
		GoogleMissing: []string{}, MicrosoftMissing: []string{},
	}
	if !c.GoogleEnabled {
		c.GoogleMissing = append(c.GoogleMissing, "GOOGLE_WORKSPACE_DELEGATION_KEY")
	}
	if s.msClientID == "" {
		c.MicrosoftMissing = append(c.MicrosoftMissing, "BOX_OUTLOOK_CLIENT_ID")
	}
	if s.msSecret == "" {
		c.MicrosoftMissing = append(c.MicrosoftMissing, "BOX_OUTLOOK_CLIENT_SECRET")
	}
	return c
}

// ---- tokens -------------------------------------------------------------

// googleSource mints tokens for subject under domain-wide delegation.
func (s *Service) googleSource(subject string, scopes ...string) (oauth2.TokenSource, error) {
	key := "g\x00" + subject + "\x00" + strings.Join(scopes, " ")
	if ts, ok := s.sources.Load(key); ok {
		return ts.(oauth2.TokenSource), nil
	}
	cfg, err := google.JWTConfigFromJSON(s.googleKey, scopes...)
	if err != nil {
		return nil, err
	}
	cfg.Subject = subject
	cfg.TokenURL = s.googleTokenURL
	ts := oauth2.ReuseTokenSource(nil, cfg.TokenSource(s.tokenContext()))
	s.sources.Store(key, ts)
	return ts, nil
}

// microsoftSource mints application tokens for a tenant.
func (s *Service) microsoftSource(tenant string) oauth2.TokenSource {
	key := "m\x00" + tenant
	if ts, ok := s.sources.Load(key); ok {
		return ts.(oauth2.TokenSource)
	}
	cfg := clientcredentials.Config{
		ClientID: s.msClientID, ClientSecret: s.msSecret,
		TokenURL: s.msLoginBase + "/" + url.PathEscape(tenant) + "/oauth2/v2.0/token",
		Scopes:   []string{"https://graph.microsoft.com/.default"},
	}
	ts := oauth2.ReuseTokenSource(nil, cfg.TokenSource(s.tokenContext()))
	s.sources.Store(key, ts)
	return ts
}

// tokenContext outlives any one request: a token source refreshes long after the call that made it.
func (s *Service) tokenContext() context.Context {
	return context.WithValue(context.Background(), oauth2.HTTPClient, s.http)
}

func (s *Service) forget(prefix string) {
	s.sources.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), prefix) {
			s.sources.Delete(k)
		}
		return true
	})
}

// tokenError turns a refusal from Google or Microsoft into what the administrator has to do.
func tokenError(provider string, err error) *errx.Error {
	var re *oauth2.RetrieveError
	code := ""
	if errors.As(err, &re) {
		// The JWT flow leaves ErrorCode empty and the reason only in the body.
		code = re.ErrorCode + " " + re.ErrorDescription + " " + truncate(string(re.Body), 500)
	}
	if provider == models.GrantProviderGoogle {
		switch {
		case strings.Contains(code, "unauthorized_client"):
			return errx.NewWithIdentifier(errx.BadRequest, ErrIDGoogleUnauthorized,
				"Google refused the delegation. In the Google Admin console, under Security > Access and data control > API controls > Domain-wide delegation, add Warmbly's client ID with exactly the scopes shown, then try again. Changes can take a few minutes to apply.")
		case strings.Contains(code, "invalid_grant"):
			return errx.NewWithIdentifier(errx.BadRequest, ErrIDMailboxUnreachable,
				"Google does not know this address in the domain, or the account is suspended.")
		}
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDGoogleUnauthorized, "Google did not issue a token for this domain. Check the domain-wide delegation entry and try again.")
	}
	switch {
	case strings.Contains(code, "AADSTS700016"), strings.Contains(code, "AADSTS65001"), strings.Contains(code, "AADSTS500011"):
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent,
			"This Microsoft 365 organization has not approved Warmbly. Ask a Global Administrator to connect it with admin consent.")
	case strings.Contains(code, "AADSTS90002"):
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent, "Microsoft does not know this organization.")
	}
	return errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent, "Microsoft did not issue a token for this organization. Connect it again with admin consent.")
}

// ---- HTTP ---------------------------------------------------------------

type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string { return fmt.Sprintf("status %d", e.status) }

// getJSON calls a Google or Graph endpoint with a token from ts.
func (s *Service) getJSON(ctx context.Context, ts oauth2.TokenSource, u string, out any) error {
	tok, err := ts.Token()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return &apiError{status: resp.StatusCode, body: truncate(string(body), 300)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---- Google -------------------------------------------------------------

type googleUser struct {
	ID           string `json:"id"`
	PrimaryEmail string `json:"primaryEmail"`
	Suspended    bool   `json:"suspended"`
	Name         struct {
		FullName string `json:"fullName"`
	} `json:"name"`
}

func (s *Service) googleDirectory(ctx context.Context, admin string, limit int) ([]googleUser, error) {
	ts, err := s.googleSource(admin, config.GoogleDirectoryScope)
	if err != nil {
		return nil, err
	}
	var out []googleUser
	page := ""
	for len(out) < limit {
		q := url.Values{"customer": {"my_customer"}, "maxResults": {"500"}, "projection": {"basic"}, "orderBy": {"email"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var resp struct {
			Users         []googleUser `json:"users"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := s.getJSON(ctx, ts, s.adminBase+"/users?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Users...)
		if resp.NextPageToken == "" {
			break
		}
		page = resp.NextPageToken
	}
	return out, nil
}

// googleProfile proves a Gmail token works for subject and returns the address Gmail knows it by.
func (s *Service) googleProfile(ctx context.Context, subject string) (string, error) {
	ts, err := s.googleSource(subject, gmail.GmailModifyScope, gmail.GmailSettingsBasicScope)
	if err != nil {
		return "", err
	}
	var p struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := s.getJSON(ctx, ts, s.gmailBase+"/users/me/profile", &p); err != nil {
		return "", err
	}
	return p.EmailAddress, nil
}

func (s *Service) verifyGoogle(ctx context.Context, domain, admin string) (*models.DomainGrant, *errx.Error) {
	if _, err := s.googleProfile(ctx, admin); err != nil {
		return nil, s.googleErr(err)
	}
	users, err := s.googleDirectory(ctx, admin, 500)
	if err != nil {
		return nil, s.googleErr(err)
	}
	domains := map[string]bool{domain: true}
	for _, u := range users {
		if at := strings.LastIndex(u.PrimaryEmail, "@"); at > 0 {
			domains[strings.ToLower(u.PrimaryEmail[at+1:])] = true
		}
	}
	return &models.DomainGrant{Provider: models.GrantProviderGoogle, Tenant: domain, AdminEmail: admin, Domains: sortedKeys(domains)}, nil
}

// unavailable is a failure that says nothing about the grant: a timeout, a 5xx, a dropped connection.
func unavailable(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status >= 500 || ae.status == http.StatusTooManyRequests
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		return re.Response != nil && re.Response.StatusCode >= 500
	}
	return true
}

func unavailableErr() *errx.Error {
	return errx.NewWithIdentifier(errx.ServiceUnavailable, ErrIDUnavailable, "The provider did not answer. Try again in a minute.")
}

func (s *Service) googleErr(err error) *errx.Error {
	if unavailable(err) {
		return unavailableErr()
	}
	var ae *apiError
	if errors.As(err, &ae) {
		if ae.status == http.StatusForbidden || ae.status == http.StatusUnauthorized {
			return errx.NewWithIdentifier(errx.BadRequest, ErrIDGoogleUnauthorized,
				"Google accepted the delegation but refused the call. Check that the administrator address is a super administrator and that every scope shown is listed in the delegation entry.")
		}
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDMailboxUnreachable, fmt.Sprintf("Google answered %d.", ae.status))
	}
	return tokenError(models.GrantProviderGoogle, err)
}

// ---- Microsoft ----------------------------------------------------------

func (s *Service) verifyMicrosoft(ctx context.Context, tenant string) (*models.DomainGrant, *errx.Error) {
	// The domains come from the users themselves: reading the organization
	// would need a permission beyond Mail.ReadWrite, Mail.Send and User.Read.All.
	users, err := s.microsoftUsers(ctx, tenant, 5000)
	if err != nil {
		return nil, s.microsoftErr(err)
	}
	domains := map[string]bool{}
	for _, u := range users {
		// A guest's own domain belongs to another organization.
		if strings.EqualFold(u.UserType, "Guest") {
			continue
		}
		for _, addr := range []string{u.Mail, u.UserPrincipalName} {
			if at := strings.LastIndex(addr, "@"); at > 0 && !strings.Contains(addr, "#EXT#") {
				domains[strings.ToLower(addr[at+1:])] = true
			}
		}
	}
	return &models.DomainGrant{Provider: models.GrantProviderMicrosoft, Tenant: tenant, Domains: sortedKeys(domains)}, nil
}

func (s *Service) microsoftErr(err error) *errx.Error {
	if unavailable(err) {
		return unavailableErr()
	}
	var ae *apiError
	if errors.As(err, &ae) {
		if ae.status == http.StatusForbidden || ae.status == http.StatusUnauthorized {
			return errx.NewWithIdentifier(errx.BadRequest, ErrIDMicrosoftConsent,
				"Microsoft issued a token but refused the call. The app registration needs the application permissions Mail.ReadWrite, Mail.Send and User.Read.All with admin consent granted.")
		}
		if ae.status == http.StatusNotFound {
			return errx.NewWithIdentifier(errx.BadRequest, ErrIDMailboxUnreachable,
				"Microsoft 365 has no mailbox for this user, or the account has no Exchange Online license.")
		}
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDMailboxUnreachable, fmt.Sprintf("Microsoft answered %d.", ae.status))
	}
	return tokenError(models.GrantProviderMicrosoft, err)
}

type graphUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	AccountEnabled    bool   `json:"accountEnabled"`
	UserType          string `json:"userType"`
}

func (s *Service) microsoftUsers(ctx context.Context, tenant string, limit int) ([]graphUser, error) {
	ts := s.microsoftSource(tenant)
	next := s.graphBase + "/users?$top=999&$select=id,displayName,mail,userPrincipalName,accountEnabled,userType"
	var out []graphUser
	for next != "" && len(out) < limit {
		var resp struct {
			Value    []graphUser `json:"value"`
			NextLink string      `json:"@odata.nextLink"`
		}
		if err := s.getJSON(ctx, ts, next, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Value...)
		next = resp.NextLink
	}
	return out, nil
}

// microsoftResolve finds the Graph user behind an address and proves its mailbox answers.
func (s *Service) microsoftResolve(ctx context.Context, tenant, email string) (*graphUser, error) {
	ts := s.microsoftSource(tenant)
	var u graphUser
	err := s.getJSON(ctx, ts, s.graphBase+"/users/"+url.PathEscape(email)+"?$select=id,displayName,mail,userPrincipalName,accountEnabled,userType", &u)
	var ae *apiError
	if errors.As(err, &ae) && ae.status == http.StatusNotFound {
		// The address may be a user's mail rather than their sign-in name.
		var list struct {
			Value []graphUser `json:"value"`
		}
		filter := url.QueryEscape("mail eq '" + strings.ReplaceAll(email, "'", "''") + "'")
		if err := s.getJSON(ctx, ts, s.graphBase+"/users?$filter="+filter+"&$select=id,displayName,mail,userPrincipalName,accountEnabled,userType", &list); err != nil {
			return nil, err
		}
		if len(list.Value) == 0 {
			return nil, &apiError{status: http.StatusNotFound}
		}
		u = list.Value[0]
	} else if err != nil {
		return nil, err
	}
	if err := s.getJSON(ctx, ts, s.graphBase+"/users/"+url.PathEscape(u.ID)+"/mailFolders/inbox?$select=id", nil); err != nil {
		return nil, err
	}
	return &u, nil
}

// ---- grants -------------------------------------------------------------

func notConfigured(what string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, ErrIDNotConfigured,
		what+" connections are not set up on this instance. See the self-hosting configuration docs.")
}

func (s *Service) get(ctx context.Context, orgID, id uuid.UUID) (*models.DomainGrant, *errx.Error) {
	g, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if g == nil {
		return nil, errx.ErrNotFound
	}
	return g, nil
}

func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]models.DomainGrant, *errx.Error) {
	list, err := s.repo.List(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return list, nil
}

func (s *Service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.DomainGrant, *errx.Error) {
	return s.get(ctx, orgID, id)
}

// Recheck verifies a grant again and puts its mailboxes back to work when it recovered.
func (s *Service) Recheck(ctx context.Context, orgID, id uuid.UUID) (*models.DomainGrant, *errx.Error) {
	g, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.check(ctx, g); xerr != nil {
		return nil, xerr
	}
	return s.get(ctx, orgID, id)
}

// check re-verifies one grant, records the verdict, and revives its mailboxes on recovery.
func (s *Service) check(ctx context.Context, g *models.DomainGrant) *errx.Error {
	var xerr *errx.Error
	switch g.Provider {
	case models.GrantProviderGoogle:
		if !s.googleEnabled() {
			return notConfigured("Google Workspace")
		}
		s.forget("g\x00" + g.AdminEmail)
		_, xerr = s.verifyGoogle(ctx, g.Tenant, g.AdminEmail)
	case models.GrantProviderMicrosoft:
		if !s.microsoftEnabled() {
			return notConfigured("Microsoft 365")
		}
		s.forget("m\x00" + g.Tenant)
		_, xerr = s.verifyMicrosoft(ctx, g.Tenant)
	}
	if xerr != nil {
		// A provider that did not answer says nothing about the grant.
		if xerr.Identifier != ErrIDUnavailable {
			_ = s.repo.SetStatus(ctx, g.ID, "invalid", xerr.Message)
		}
		return xerr
	}
	_ = s.repo.SetStatus(ctx, g.ID, "active", "")
	ids, err := s.repo.InactiveMailboxes(ctx, g.ID)
	if err == nil {
		for _, id := range ids {
			if _, xerr := s.mailboxes.ReactivateDelegated(ctx, id); xerr != nil {
				log.Warn().Str("email_account_id", id.String()).Str("error", xerr.Message).Msg("delegation: reactivating a mailbox failed")
			}
		}
	}
	return nil
}

// Delete forgets a grant; its mailboxes stop, since nothing can mint for them any more.
func (s *Service) Delete(ctx context.Context, orgID, userID, id uuid.UUID) *errx.Error {
	g, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	ids, err := s.repo.Delete(ctx, orgID, id)
	if err != nil {
		return errx.InternalError()
	}
	inactive := "inactive"
	for _, a := range ids {
		if _, xerr := s.mailboxes.Update(ctx, orgID.String(), userID.String(), a.String(), &models.UpdateEmail{Status: &inactive}); xerr != nil {
			log.Warn().Str("email_account_id", a.String()).Str("error", xerr.Message).Msg("delegation: stopping a mailbox failed")
		}
	}
	if g.Provider == models.GrantProviderMicrosoft {
		s.forget("m\x00" + g.Tenant)
	}
	return nil
}

// Users lists the granted directory, marking who is already connected here.
func (s *Service) Users(ctx context.Context, orgID, id uuid.UUID) ([]models.DirectoryUser, *errx.Error) {
	g, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	var out []models.DirectoryUser
	switch g.Provider {
	case models.GrantProviderGoogle:
		users, err := s.googleDirectory(ctx, g.AdminEmail, 10000)
		if err != nil {
			return nil, s.googleErr(err)
		}
		for _, u := range users {
			out = append(out, models.DirectoryUser{ID: strings.ToLower(u.PrimaryEmail), Email: u.PrimaryEmail, Name: u.Name.FullName, Enabled: !u.Suspended})
		}
	case models.GrantProviderMicrosoft:
		users, err := s.microsoftUsers(ctx, g.Tenant, 10000)
		if err != nil {
			return nil, s.microsoftErr(err)
		}
		for _, u := range users {
			if u.Mail == "" || strings.EqualFold(u.UserType, "Guest") {
				continue
			}
			out = append(out, models.DirectoryUser{ID: u.ID, Email: u.Mail, Name: u.DisplayName, Enabled: u.AccountEnabled})
		}
	}
	// The directory can hold domains the grant does not cover; they are not this workspace's.
	covered := out[:0]
	for _, u := range out {
		if covers(g, u.Email) {
			covered = append(covered, u)
		}
	}
	out = covered
	emails := make([]string, len(out))
	for i := range out {
		emails[i] = out[i].Email
	}
	existing, xerr := s.store.FindManyInOrganization(ctx, orgID, emails)
	if xerr != nil {
		return nil, xerr
	}
	provider := models.InboxProviderGoogle
	if g.Provider == models.GrantProviderMicrosoft {
		provider = models.InboxProviderOutlook
	}
	for i := range out {
		if ref, ok := existing[strings.ToLower(out[i].Email)]; ok {
			id := ref.ID
			out[i].Connected, out[i].AccountID = true, &id
			out[i].Upgrade = ref.Movable(provider)
		}
	}
	if out == nil {
		out = []models.DirectoryUser{}
	}
	return out, nil
}

// GrantFor is the workspace's active grant covering a domain, nil when none.
func (s *Service) GrantFor(ctx context.Context, orgID uuid.UUID, provider, domain string) (*models.DomainGrant, error) {
	if (provider == models.GrantProviderGoogle && !s.googleEnabled()) || (provider == models.GrantProviderMicrosoft && !s.microsoftEnabled()) {
		return nil, nil
	}
	return s.repo.ForDomain(ctx, orgID, provider, domain)
}

// Connect proves a token works for one address under a grant and stores the mailbox.
func (s *Service) Connect(ctx context.Context, orgID uuid.UUID, userID string, grantID uuid.UUID, email, name string) (*models.Email, *errx.Error) {
	g, xerr := s.get(ctx, orgID, grantID)
	if xerr != nil {
		return nil, xerr
	}
	if g.Status != "active" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDGrantInactive, "The administrator's grant for this domain stopped working. Check it, then retry.")
	}
	email = strings.TrimSpace(email)
	domain := ""
	if at := strings.LastIndex(email, "@"); at > 0 {
		domain = strings.ToLower(email[at+1:])
	}
	if domain == "" || !covers(g, email) {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNotCovered, "This address is not on a domain the grant covers.")
	}
	data := models.NewDelegatedAccount{Email: email, Name: name, GrantID: g.ID}
	switch g.Provider {
	case models.GrantProviderGoogle:
		addr, err := s.googleProfile(ctx, strings.ToLower(email))
		if err != nil {
			return nil, s.googleErr(err)
		}
		data.Provider, data.Subject, data.Email = models.InboxProviderGoogle, strings.ToLower(addr), addr
		data.MailHost = "google_workspace"
	case models.GrantProviderMicrosoft:
		u, err := s.microsoftResolve(ctx, g.Tenant, email)
		if err != nil {
			return nil, s.microsoftErr(err)
		}
		if !u.AccountEnabled {
			return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDMailboxUnreachable, "This Microsoft 365 account is disabled.")
		}
		data.Provider, data.Subject, data.MailHost = models.InboxProviderOutlook, u.ID, "microsoft365"
		if data.Name == "" {
			data.Name = u.DisplayName
		}
	}
	return s.mailboxes.ConnectDelegated(ctx, userID, &orgID, data)
}

// AccessToken is a worker's token for a delegated mailbox; handled is false
// for any other mailbox, so the caller can try its own broker.
func (s *Service) AccessToken(ctx context.Context, accountID uuid.UUID) (*models.PoolLinkAccessToken, bool, *errx.Error) {
	d, xerr := s.store.GetDelegation(ctx, accountID)
	if xerr != nil {
		return nil, true, xerr
	}
	if d == nil {
		return nil, false, nil
	}
	g, err := s.repo.GetByID(ctx, d.GrantID)
	if err != nil {
		return nil, true, errx.InternalError()
	}
	if g == nil || g.OrganizationID != d.OrganizationID || !covers(g, d.Email) {
		return nil, true, errx.ErrNotFound
	}
	if g.Status != "active" {
		return nil, true, errx.NewWithIdentifier(errx.Forbidden, ErrIDGrantInactive, "The administrator's grant for this mailbox is not active.")
	}
	var ts oauth2.TokenSource
	switch d.Provider {
	case models.InboxProviderGoogle:
		if !s.googleEnabled() {
			return nil, true, notConfigured("Google Workspace")
		}
		src, err := s.googleSource(d.Subject, gmail.GmailModifyScope, gmail.GmailSettingsBasicScope)
		if err != nil {
			return nil, true, errx.InternalError()
		}
		ts = src
	case models.InboxProviderOutlook:
		if !s.microsoftEnabled() {
			return nil, true, notConfigured("Microsoft 365")
		}
		ts = s.microsoftSource(g.Tenant)
	default:
		return nil, true, errx.ErrNotFound
	}
	tok, err := ts.Token()
	if err != nil {
		provider := models.GrantProviderGoogle
		if d.Provider == models.InboxProviderOutlook {
			provider = models.GrantProviderMicrosoft
		}
		xerr := tokenError(provider, err)
		return nil, true, errx.NewWithIdentifier(errx.Forbidden, xerr.Identifier, xerr.Message)
	}
	return &models.PoolLinkAccessToken{AccessToken: tok.AccessToken, ExpiresAt: tok.Expiry, Provider: string(d.Provider), Email: d.Email}, true, nil
}

// StartHealth re-verifies every grant hourly, so a revoked one is noticed and a restored one revives its mailboxes.
func (s *Service) StartHealth(ctx context.Context) {
	jobrun.Loop(ctx, "mailbox_grant_health", time.Hour, false, func(ctx context.Context) error {
		grants, err := s.repo.ListActive(ctx)
		if err != nil {
			return err
		}
		for i := range grants {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_ = s.check(cctx, &grants[i])
			cancel()
		}
		return nil
	})
}

// covers reports whether an address is on a domain the grant verified.
func covers(g *models.DomainGrant, email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range g.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

func (s *Service) directoryScope() string { return config.GoogleDirectoryScope }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SigninMigration is every mailbox still on per-mailbox Google sign-in, by
// domain, with where it moves: the domain's grant, or an app password for a
// shared address like gmail.com.
func (s *Service) SigninMigration(ctx context.Context, orgID uuid.UUID) ([]models.SigninMigration, *errx.Error) {
	boxes, xerr := s.store.ListSigninRetiring(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	byDomain := map[string]*models.SigninMigration{}
	var order []string
	for _, b := range boxes {
		domain := ""
		if at := strings.LastIndex(b.Email, "@"); at > 0 {
			domain = strings.ToLower(b.Email[at+1:])
		}
		m, ok := byDomain[domain]
		if !ok {
			m = &models.SigninMigration{Domain: domain, Kind: "workspace"}
			if mailhost.SharedProvider(domain) {
				m.Kind = "personal"
			} else if g, err := s.GrantFor(ctx, orgID, models.GrantProviderGoogle, domain); err == nil && g != nil && g.Status == "active" {
				id := g.ID
				m.GrantID = &id
			}
			byDomain[domain] = m
			order = append(order, domain)
		}
		m.Mailboxes = append(m.Mailboxes, b)
	}
	out := make([]models.SigninMigration, 0, len(order))
	for _, d := range order {
		out = append(out, *byDomain[d])
	}
	return out, nil
}
