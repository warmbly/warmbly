package delegation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
)

const adminPassword = "Correct-Horse-9!"

// adminFake is Microsoft for the automatic consent: a password sign-in for the admin mailbox, Graph's service
// principals and app role assignments, and an app whose client-credentials token works once all three roles are assigned.
type adminFake struct {
	mu sync.Mutex
	// signin is what the password grant answers: "ok", "mfa", "password", "blocked" or "down".
	signin string
	// tid and wids are the signed-in admin's token claims; roles is what /me/memberOf lists.
	tid   string
	wids  []any
	roles []string
	// sps are the tenant's service principals by appId; assigned is each one's app role ids.
	sps      map[string]string
	assigned map[string][]string
	// refuseProbes refuses that many client-credentials tokens, as a consent still on its way does.
	refuseProbes int

	passwordGrants int
	spCreates      int
	assignPosts    int
	forms          []map[string]string
}

var graphRoles = map[string]string{"Mail.ReadWrite": "role-rw", "Mail.Send": "role-send", "User.Read.All": "role-users", "Mail.Read": "role-read"}

func newAdminFake(t *testing.T, s *Service) *adminFake {
	t.Helper()
	f := &adminFake{signin: "ok", tid: acmeTenant, wids: []any{"62e90394-69f5-4237-9190-012177145e10"},
		sps: map[string]string{msGraphAppID: "sp-graph"}, assigned: map[string][]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = r.ParseForm()
		auth := r.Header.Get("Authorization")
		switch p := r.URL.Path; {
		case p == "/ms/getacme.com/v2.0/.well-known/openid-configuration":
			writeJSON(w, map[string]any{"issuer": "https://login.microsoftonline.com/" + acmeTenant + "/v2.0"})
		case p == "/ms/"+acmeTenant+"/oauth2/v2.0/token" && r.Form.Get("grant_type") == "password":
			f.passwordGrants++
			form := map[string]string{}
			for k := range r.Form {
				form[k] = r.Form.Get(k)
			}
			f.forms = append(f.forms, form)
			f.passwordSignin(w, r.Form.Get("password"))
		case p == "/ms/"+acmeTenant+"/oauth2/v2.0/token":
			if !f.consentedLocked() || f.refuseProbes > 0 {
				f.refuseProbes = max(0, f.refuseProbes-1)
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"AADSTS7000229: The client application is missing service principal in the tenant."}`))
				return
			}
			writeJSON(w, map[string]any{"access_token": "app", "token_type": "Bearer", "expires_in": 3600})
		case p == "/graph/users" && auth == "Bearer app":
			writeJSON(w, map[string]any{"value": []any{map[string]any{"id": "u-1", "mail": "ada@getacme.com", "userPrincipalName": "ada@getacme.com", "accountEnabled": true}}})
		case !strings.HasPrefix(auth, "Bearer admin."):
			w.WriteHeader(http.StatusUnauthorized)
		case p == "/graph/me/memberOf/microsoft.graph.directoryRole":
			var out []any
			for _, id := range f.roles {
				out = append(out, map[string]any{"roleTemplateId": id})
			}
			writeJSON(w, map[string]any{"value": out})
		case p == "/graph/servicePrincipals" && r.Method == http.MethodGet:
			appID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Query().Get("$filter"), "appId eq '"), "'")
			id, ok := f.sps[appID]
			if !ok {
				writeJSON(w, map[string]any{"value": []any{}})
				return
			}
			sp := map[string]any{"id": id}
			if appID == msGraphAppID {
				var roles []any
				for v, rid := range graphRoles {
					roles = append(roles, map[string]any{"id": rid, "value": v, "allowedMemberTypes": []string{"Application"}})
				}
				sp["appRoles"] = roles
			}
			writeJSON(w, map[string]any{"value": []any{sp}})
		case p == "/graph/servicePrincipals" && r.Method == http.MethodPost:
			var in struct {
				AppID string `json:"appId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.spCreates++
			f.sps[in.AppID] = "sp-" + in.AppID
			writeJSON(w, map[string]any{"id": f.sps[in.AppID]})
		case strings.HasSuffix(p, "/appRoleAssignments"):
			sp := strings.TrimSuffix(strings.TrimPrefix(p, "/graph/servicePrincipals/"), "/appRoleAssignments")
			if r.Method == http.MethodGet {
				var out []any
				for _, rid := range f.assigned[sp] {
					out = append(out, map[string]any{"resourceId": "sp-graph", "appRoleId": rid})
				}
				writeJSON(w, map[string]any{"value": out})
				return
			}
			var in struct{ PrincipalID, ResourceID, AppRoleID string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.assignPosts++
			if in.PrincipalID != sp || in.ResourceID != "sp-graph" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if slices.Contains(f.assigned[sp], in.AppRoleID) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"Permission being assigned already exists on the object"}}`))
				return
			}
			f.assigned[sp] = append(f.assigned[sp], in.AppRoleID)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{"id": "a"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	s.msLoginBase, s.graphBase = srv.URL+"/ms", srv.URL+"/graph"
	s.consentPause = 0
	return f
}

func (f *adminFake) passwordSignin(w http.ResponseWriter, password string) {
	refuse := func(status int, code int, desc string) {
		w.WriteHeader(status)
		// Microsoft does not echo the password; a hostile or broken endpoint might, and nothing may repeat it.
		writeJSON(w, map[string]any{"error": "invalid_grant", "error_description": "AADSTS" + itoa(code) + ": " + desc + " " + password, "error_codes": []int{code}})
	}
	switch {
	case f.signin == "down":
		w.WriteHeader(http.StatusServiceUnavailable)
	case f.signin == "mfa":
		refuse(http.StatusBadRequest, 50076, "Due to a configuration change made by your administrator, you must use multi-factor authentication.")
	case f.signin == "blocked":
		refuse(http.StatusUnauthorized, 7000218, "The request body must contain the following parameter: 'client_assertion' or 'client_secret'.")
	case f.signin == "password" || password != adminPassword:
		refuse(http.StatusBadRequest, 50126, "Error validating credentials due to invalid username or password.")
	default:
		claims := map[string]any{"tid": f.tid, "upn": "admin@getacme.com"}
		if f.wids != nil {
			claims["wids"] = f.wids
		}
		// The fake recognises its own tokens by the "admin." prefix.
		writeJSON(w, map[string]any{"access_token": "admin." + strings.SplitN(fakeIDToken(claims), ".", 2)[1], "token_type": "Bearer", "expires_in": 3600})
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (f *adminFake) consentedLocked() bool {
	sp, ok := f.sps["app-id"]
	if !ok {
		return false
	}
	for _, v := range microsoftAppRoles {
		if !slices.Contains(f.assigned[sp], graphRoles[v]) {
			return false
		}
	}
	return true
}

func autoHarness(t *testing.T) (*Service, *adminFake, *memGrants, *wakeLog) {
	t.Helper()
	s, _, grants, _, _ := newTestService(t)
	f := newAdminFake(t, s)
	w := &wakeLog{}
	s.WireConsentWaker(w.wake)
	return s, f, grants, w
}

func autoReason(t *testing.T, err error) *AutoConsentError {
	t.Helper()
	var ae *AutoConsentError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want an AutoConsentError", err)
	}
	return ae
}

// The admin mailbox's sign-in creates the app's service principal, assigns it the three Graph roles, records the
// workspace's grant and wakes the connects waiting on it.
func TestAutoConsentApprovesTheTenant(t *testing.T) {
	s, f, grants, wakes := autoHarness(t)
	org, user := uuid.New(), uuid.New()
	if err := s.ConsentPurchasedTenantWithAdmin(context.Background(), org, user, "GetAcme.com", "admin@getacme.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	if f.passwordGrants != 1 || f.spCreates != 1 || f.assignPosts != 3 || len(f.assigned["sp-app-id"]) != 3 {
		t.Fatalf("grants=%d creates=%d posts=%d assigned=%v", f.passwordGrants, f.spCreates, f.assignPosts, f.assigned)
	}
	form := f.forms[0]
	if form["client_id"] != msAdminClientID || form["scope"] != "https://graph.microsoft.com/.default" || form["username"] != "admin@getacme.com" || form["password"] != adminPassword {
		t.Fatalf("password grant = client %q scope %q user %q", form["client_id"], form["scope"], form["username"])
	}
	if slices.Contains(f.assigned["sp-app-id"], graphRoles["Mail.Read"]) {
		t.Fatal("assigned a role the app does not run on")
	}
	if len(grants.grants) != 1 {
		t.Fatalf("grants = %d", len(grants.grants))
	}
	for _, g := range grants.grants {
		if g.OrganizationID != org || g.Provider != models.GrantProviderMicrosoft || g.Tenant != acmeTenant || !g.Purchased || !slices.Contains(g.Domains, "getacme.com") {
			t.Fatalf("grant = %+v", g)
		}
	}
	if len(wakes.calls) != 1 || !strings.HasPrefix(wakes.calls[0], org.String()+" ") {
		t.Fatalf("wakes = %v", wakes.calls)
	}
}

// What a previous run left is kept: nothing is created or assigned twice, and a tenant already working needs no sign-in.
func TestAutoConsentIsIdempotent(t *testing.T) {
	s, f, grants, _ := autoHarness(t)
	ctx := context.Background()
	org := uuid.New()
	// A run that stopped after the service principal and one role.
	f.sps["app-id"], f.assigned["sp-app-id"] = "sp-app-id", []string{graphRoles["Mail.ReadWrite"]}
	if err := s.ConsentPurchasedTenantWithAdmin(ctx, org, uuid.New(), "getacme.com", "admin@getacme.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	if f.spCreates != 0 || f.assignPosts != 2 || len(f.assigned["sp-app-id"]) != 3 {
		t.Fatalf("creates=%d posts=%d assigned=%v", f.spCreates, f.assignPosts, f.assigned)
	}
	// The same again while the token endpoint has not caught up: signs in, changes nothing.
	// Two refusals: oauth2 sends the client secret in the header, then again in the body.
	f.refuseProbes = 2
	s.forget("m\x00")
	if err := s.ConsentPurchasedTenantWithAdmin(ctx, org, uuid.New(), "getacme.com", "admin@getacme.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	if f.passwordGrants != 2 || f.spCreates != 0 || f.assignPosts != 2 || len(f.assigned["sp-app-id"]) != 3 {
		t.Fatalf("rerun: grants=%d creates=%d posts=%d assigned=%v", f.passwordGrants, f.spCreates, f.assignPosts, f.assigned)
	}
	// Once the app works there, nobody signs in at all.
	if err := s.ConsentPurchasedTenantWithAdmin(ctx, org, uuid.New(), "getacme.com", "admin@getacme.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	if f.passwordGrants != 2 || len(grants.grants) != 1 {
		t.Fatalf("approved tenant: password grants=%d grants=%d", f.passwordGrants, len(grants.grants))
	}
}

func TestAutoConsentRefusals(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(f *adminFake)
		reason string
		retry  bool
	}{
		{"another tenant's admin", func(f *adminFake) { f.tid = otherTenant }, models.ConsentReasonWrongTenant, false},
		{"not an administrator", func(f *adminFake) { f.wids, f.roles = nil, []string{"b79fbf4d-3ef9-4689-8143-76b194e85509"} }, models.ConsentReasonNotAdmin, false},
		{"MFA required", func(f *adminFake) { f.signin = "mfa" }, models.ConsentReasonMFA, false},
		{"wrong password", func(f *adminFake) { f.signin = "password" }, models.ConsentReasonPassword, false},
		{"password grant blocked", func(f *adminFake) { f.signin = "blocked" }, models.ConsentReasonBlocked, false},
		{"Microsoft unavailable", func(f *adminFake) { f.signin = "down" }, AutoConsentUnavailable, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, f, grants, wakes := autoHarness(t)
			tc.setup(f)
			ae := autoReason(t, s.ConsentPurchasedTenantWithAdmin(context.Background(), uuid.New(), uuid.New(), "getacme.com", "admin@getacme.com", adminPassword))
			if ae.Reason != tc.reason || ae.Retryable() != tc.retry {
				t.Fatalf("reason = %s retryable = %v, want %s %v", ae.Reason, ae.Retryable(), tc.reason, tc.retry)
			}
			if f.spCreates != 0 || f.assignPosts != 0 || len(grants.grants) != 0 || len(wakes.calls) != 0 {
				t.Fatalf("creates=%d posts=%d grants=%d wakes=%v", f.spCreates, f.assignPosts, len(grants.grants), wakes.calls)
			}
		})
	}
}

// A role found through Graph when the token does not list it is as good as one in the token.
func TestAutoConsentReadsRolesFromGraph(t *testing.T) {
	s, f, grants, _ := autoHarness(t)
	f.wids, f.roles = nil, []string{"e8611ab8-c189-46e8-94e1-60213ab1f814"}
	if err := s.ConsentPurchasedTenantWithAdmin(context.Background(), uuid.New(), uuid.New(), "getacme.com", "admin@getacme.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	if len(grants.grants) != 1 {
		t.Fatalf("grants = %d", len(grants.grants))
	}
}

// The password reaches Microsoft's token endpoint and nothing else: no error, no log line.
func TestAutoConsentNeverRepeatsThePassword(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prev })
	for _, mode := range []string{"mfa", "password", "blocked", "down", "ok-wrong-tenant", "ok-not-admin"} {
		s, f, _, _ := autoHarness(t)
		switch mode {
		case "ok-wrong-tenant":
			f.tid = otherTenant
		case "ok-not-admin":
			f.wids = nil
		default:
			f.signin = mode
		}
		err := s.ConsentPurchasedTenantWithAdmin(context.Background(), uuid.New(), uuid.New(), "getacme.com", "admin@getacme.com", adminPassword)
		if err == nil || strings.Contains(err.Error(), adminPassword) || strings.Contains(err.Error(), "admin.") {
			t.Fatalf("%s: err = %v", mode, err)
		}
	}
	// A refusal whose own error field is not an OAuth code is logged as "refused".
	if _, code := passwordRefusal(tokenRefusal{Error: adminPassword}); code != "refused" {
		t.Fatalf("code = %q", code)
	}
	if buf.Len() == 0 || strings.Contains(buf.String(), adminPassword) || strings.Contains(buf.String(), "admin@getacme.com") {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestPasswordRefusalReasons(t *testing.T) {
	for _, tc := range []struct {
		in   tokenRefusal
		want string
	}{
		{tokenRefusal{Error: "invalid_grant", Codes: []int{50079}}, models.ConsentReasonMFA},
		{tokenRefusal{Error: "invalid_grant", Codes: []int{53003}}, models.ConsentReasonMFA},
		{tokenRefusal{Error: "interaction_required"}, models.ConsentReasonMFA},
		{tokenRefusal{Error: "invalid_grant", Description: "AADSTS50158: External security challenge not satisfied."}, models.ConsentReasonMFA},
		{tokenRefusal{Error: "invalid_grant", Codes: []int{50126}}, models.ConsentReasonPassword},
		{tokenRefusal{Error: "invalid_client", Codes: []int{9001023}}, models.ConsentReasonBlocked},
		{tokenRefusal{Error: "unsupported_grant_type"}, models.ConsentReasonBlocked},
		{tokenRefusal{Error: "temporarily_unavailable"}, AutoConsentUnavailable},
		{tokenRefusal{Error: "invalid_request", Codes: []int{90002}}, models.ConsentReasonFailed},
	} {
		if got, _ := passwordRefusal(tc.in); got != tc.want {
			t.Errorf("%+v = %s, want %s", tc.in, got, tc.want)
		}
	}
}
