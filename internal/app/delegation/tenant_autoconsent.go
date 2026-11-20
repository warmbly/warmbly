package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// msAdminClientID is the Azure CLI: a Microsoft first-party public client that accepts the password grant and is
// pre-authorized for Graph's Directory.AccessAsUser.All.
const msAdminClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"

// msGraphAppID is Microsoft Graph's application id, the same in every tenant.
const msGraphAppID = "00000003-0000-0000-c000-000000000000"

// AutoConsentUnavailable is an automatic consent Microsoft did not answer, the only failure worth retrying soon.
const AutoConsentUnavailable = "unavailable"

// AutoConsentError is why ConsentPurchasedTenantWithAdmin stopped: AutoConsentUnavailable, or a models.ConsentReason*
// that leaves the approval to a person. It never carries a credential or a token.
type AutoConsentError struct {
	Reason string
	// code is Microsoft's AADSTS number or an HTTP status, for logs.
	code string
}

func (e *AutoConsentError) Error() string {
	return "microsoft tenant consent: " + e.Reason + " (" + e.code + ")"
}

// Retryable reports whether the consent should run again soon rather than fall back to the administrator's link.
func (e *AutoConsentError) Retryable() bool { return e.Reason == AutoConsentUnavailable }

func autoErr(reason, code string) *AutoConsentError {
	return &AutoConsentError{Reason: reason, code: code}
}

// ConsentPurchasedTenantWithAdmin is the administrator's consent made with the admin mailbox's own sign-in, whose
// password goes to Microsoft's token endpoint only. One run per workspace and tenant; a caller meanwhile shares it.
func (s *Service) ConsentPurchasedTenantWithAdmin(ctx context.Context, orgID, userID uuid.UUID, domain, adminEmail, adminPassword string) error {
	if !s.microsoftEnabled() {
		return autoErr(models.ConsentReasonFailed, "not_configured")
	}
	domain, ok := cleanConsentDomain(domain)
	if !ok || strings.TrimSpace(adminEmail) == "" || adminPassword == "" {
		return autoErr(models.ConsentReasonNoSignIn, "no_credentials")
	}
	tenant, err := s.microsoftTenant(ctx, domain)
	if err != nil {
		return autoErr(AutoConsentUnavailable, "tenant_lookup")
	}
	_, err, _ = s.autoConsents.Do(orgID.String()+"\x00"+tenant, func() (any, error) {
		return nil, s.autoConsent(ctx, orgID, userID, domain, tenant, strings.TrimSpace(adminEmail), adminPassword)
	})
	if err != nil {
		var ae *AutoConsentError
		if errors.As(err, &ae) {
			log.Info().Str("org_id", orgID.String()).Str("reason", ae.Reason).Str("code", ae.code).Msg("delegation: automatic tenant consent stopped")
		}
		return err
	}
	return nil
}

func (s *Service) autoConsent(ctx context.Context, orgID, userID uuid.UUID, domain, tenant, adminEmail, adminPassword string) error {
	// A tenant where the app already works needs no sign-in at all.
	if s.microsoftProbe(ctx, tenant) == nil {
		return autoFinish(s.finishTenantConsent(ctx, orgID, userID, domain, tenant))
	}
	access, err := s.adminPasswordToken(ctx, tenant, adminEmail, adminPassword)
	if err != nil {
		return err
	}
	claims, _ := jwtClaims(access)
	if !strings.EqualFold(claimString(claims, "tid"), tenant) {
		return autoErr(models.ConsentReasonWrongTenant, "tid")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: access, TokenType: "Bearer"})
	if !microsoftConsentAdmin(claims) {
		if err := s.graphDirectoryAdmin(ctx, ts); err != nil {
			return err
		}
	}
	if err := s.assignAppRoles(ctx, ts); err != nil {
		return err
	}
	return autoFinish(s.finishTenantConsent(ctx, orgID, userID, domain, tenant))
}

// autoFinish reads the verification after the roles are assigned: a tenant still catching up is retried, not handed on.
func autoFinish(xerr *errx.Error) error {
	if xerr == nil {
		return nil
	}
	return autoErr(AutoConsentUnavailable, xerr.Identifier)
}

// tokenRefusal is the token endpoint's error answer.
type tokenRefusal struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
	Codes       []int  `json:"error_codes"`
}

// Microsoft's refusals of a password sign-in, by what they leave a person to do.
var (
	aadInteraction = []int{50072, 50074, 50076, 50079, 50097, 50158, 53000, 53001, 53003}
	aadPassword    = []int{50034, 50055, 50056, 50126, 50144}
	aadBlocked     = []int{50053, 50057, 65002, 7000112, 7000218, 9001023}
)

// adminPasswordToken signs in as the admin mailbox with the password grant and returns a Graph access token.
func (s *Service) adminPasswordToken(ctx context.Context, tenant, username, password string) (string, error) {
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {msAdminClientID},
		"scope":      {"https://graph.microsoft.com/.default"},
		"username":   {username},
		"password":   {password},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.msLoginBase+"/"+url.PathEscape(tenant)+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", autoErr(models.ConsentReasonFailed, "request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", autoErr(AutoConsentUnavailable, "network")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusOK {
		var tok struct {
			AccessToken string `json:"access_token"`
		}
		if json.Unmarshal(raw, &tok) != nil || tok.AccessToken == "" {
			return "", autoErr(models.ConsentReasonFailed, "no_token")
		}
		return tok.AccessToken, nil
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return "", autoErr(AutoConsentUnavailable, strconv.Itoa(resp.StatusCode))
	}
	var r tokenRefusal
	_ = json.Unmarshal(raw, &r)
	return "", autoErr(passwordRefusal(r))
}

var (
	aadCode = regexp.MustCompile(`AADSTS(\d+)`)
	// oauthErrorCode is the shape of an OAuth error code, the only part of a refusal's own words that reaches a log.
	oauthErrorCode = regexp.MustCompile(`^[a-z_]{1,40}$`)
)

// passwordRefusal sorts a refused password sign-in into the reason a person reads, and the code behind it.
func passwordRefusal(r tokenRefusal) (string, string) {
	codes := r.Codes
	if m := aadCode.FindStringSubmatch(r.Description); len(codes) == 0 && m != nil {
		if c, err := strconv.Atoi(m[1]); err == nil {
			codes = []int{c}
		}
	}
	code := "refused"
	if oauthErrorCode.MatchString(r.Error) {
		code = r.Error
	}
	if len(codes) > 0 {
		code = "AADSTS" + strconv.Itoa(codes[0])
	}
	has := func(list []int) bool {
		return slices.ContainsFunc(codes, func(c int) bool { return slices.Contains(list, c) })
	}
	switch {
	case has(aadInteraction), r.Error == "interaction_required":
		return models.ConsentReasonMFA, code
	case has(aadPassword):
		return models.ConsentReasonPassword, code
	case has(aadBlocked), r.Error == "unsupported_grant_type", r.Error == "unauthorized_client":
		return models.ConsentReasonBlocked, code
	case r.Error == "temporarily_unavailable":
		return AutoConsentUnavailable, code
	}
	return models.ConsentReasonFailed, code
}

// graphDirectoryAdmin asks Graph for the signed-in user's directory roles when the token did not list them.
func (s *Service) graphDirectoryAdmin(ctx context.Context, ts oauth2.TokenSource) error {
	var roles struct {
		Value []struct {
			RoleTemplateID string `json:"roleTemplateId"`
		} `json:"value"`
	}
	if err := s.getJSON(ctx, ts, s.graphBase+"/me/memberOf/microsoft.graph.directoryRole?$select=roleTemplateId", &roles); err != nil {
		return graphAutoErr(err)
	}
	for _, r := range roles.Value {
		if microsoftAdminRoles[strings.ToLower(r.RoleTemplateID)] {
			return nil
		}
	}
	return autoErr(models.ConsentReasonNotAdmin, "roles")
}

type graphServicePrincipal struct {
	ID       string `json:"id"`
	AppRoles []struct {
		ID                 string   `json:"id"`
		Value              string   `json:"value"`
		AllowedMemberTypes []string `json:"allowedMemberTypes"`
	} `json:"appRoles"`
}

// assignAppRoles is the admin consent itself: the app's service principal granted microsoftAppRoles on Graph, idempotently.
func (s *Service) assignAppRoles(ctx context.Context, ts oauth2.TokenSource) error {
	app, err := s.servicePrincipal(ctx, ts, s.msClientID)
	if err != nil {
		return graphAutoErr(err)
	}
	if app == nil {
		var created graphServicePrincipal
		if err := s.doJSON(ctx, ts, http.MethodPost, s.graphBase+"/servicePrincipals", map[string]string{"appId": s.msClientID}, &created); err != nil {
			// Another run may have created it meanwhile.
			if app, _ = s.servicePrincipal(ctx, ts, s.msClientID); app == nil {
				return graphAutoErr(err)
			}
		} else {
			app = &created
		}
	}
	graph, err := s.servicePrincipal(ctx, ts, msGraphAppID)
	if err != nil {
		return graphAutoErr(err)
	}
	if graph == nil || app.ID == "" {
		return autoErr(models.ConsentReasonFailed, "no_graph")
	}
	var assigned struct {
		Value []struct {
			ResourceID string `json:"resourceId"`
			AppRoleID  string `json:"appRoleId"`
		} `json:"value"`
	}
	if err := s.getJSON(ctx, ts, s.graphBase+"/servicePrincipals/"+url.PathEscape(app.ID)+"/appRoleAssignments?$top=999", &assigned); err != nil {
		return graphAutoErr(err)
	}
	have := map[string]bool{}
	for _, a := range assigned.Value {
		have[strings.ToLower(a.ResourceID+"/"+a.AppRoleID)] = true
	}
	for _, value := range microsoftAppRoles {
		roleID := ""
		for _, r := range graph.AppRoles {
			if r.Value == value && slices.ContainsFunc(r.AllowedMemberTypes, func(t string) bool { return strings.EqualFold(t, "Application") }) {
				roleID = r.ID
			}
		}
		if roleID == "" {
			return autoErr(models.ConsentReasonFailed, "no_role")
		}
		if have[strings.ToLower(graph.ID+"/"+roleID)] {
			continue
		}
		body := map[string]string{"principalId": app.ID, "resourceId": graph.ID, "appRoleId": roleID}
		if err := s.doJSON(ctx, ts, http.MethodPost, s.graphBase+"/servicePrincipals/"+url.PathEscape(app.ID)+"/appRoleAssignments", body, nil); err != nil && !assignmentExists(err) {
			return graphAutoErr(err)
		}
	}
	return nil
}

// servicePrincipal is the tenant's service principal for appID, nil when the tenant has none.
func (s *Service) servicePrincipal(ctx context.Context, ts oauth2.TokenSource, appID string) (*graphServicePrincipal, error) {
	q := url.Values{"$filter": {"appId eq '" + appID + "'"}, "$select": {"id,appRoles"}}
	var out struct {
		Value []graphServicePrincipal `json:"value"`
	}
	if err := s.getJSON(ctx, ts, s.graphBase+"/servicePrincipals?"+strings.ReplaceAll(q.Encode(), "+", "%20"), &out); err != nil {
		return nil, err
	}
	if len(out.Value) == 0 {
		return nil, nil
	}
	return &out.Value[0], nil
}

// assignmentExists is Graph refusing an app role assignment the service principal already holds.
func assignmentExists(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.status == http.StatusConflict ||
		(ae.status == http.StatusBadRequest && strings.Contains(strings.ToLower(ae.body), "already exists")))
}

// graphAutoErr reads a refused Graph call made as the admin mailbox.
func graphAutoErr(err error) error {
	if unavailable(err) {
		return autoErr(AutoConsentUnavailable, "graph")
	}
	var ae *apiError
	if errors.As(err, &ae) && (ae.status == http.StatusForbidden || ae.status == http.StatusUnauthorized) {
		return autoErr(models.ConsentReasonNotAdmin, strconv.Itoa(ae.status))
	}
	if errors.As(err, &ae) {
		return autoErr(models.ConsentReasonFailed, strconv.Itoa(ae.status))
	}
	return autoErr(models.ConsentReasonFailed, "graph")
}
