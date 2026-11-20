package vendorconn

import (
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
	"github.com/warmbly/warmbly/internal/repository"
)

// Split so the addresses read as fixtures, not as anyone's mail.
const (
	avaAddr = "ava" + "@acme.io"
	leoAddr = "leo" + "@acme.io"
)

func TestDecideSigninOffersOnlySwitchesThatCanSucceed(t *testing.T) {
	full, modify := []string{models.GoogleFullMailScope}, []string{"https://www.googleapis.com/auth/gmail.modify"}
	zap := &models.VendorConnection{ID: uuid.New(), Vendor: mailvendor.VendorZapmail, Label: "Zapmail", Status: "active"}
	grant := &models.MailboxSigninGrant{ID: uuid.New(), Domain: "acme.io"}
	on := signinInputs{clientID: "client-1", grant: grant, automatic: true}
	google := func(auth string, scopes []string, v *models.VendorConnection) models.MailboxSigninState {
		return models.MailboxSigninState{Email: avaAddr, Provider: models.InboxProviderGoogle, AuthMethod: auth, MailHost: "google_workspace",
			Status: "active", GrantedScopes: scopes, Vendor: v, VendorMailboxID: "v1"}
	}
	type want struct {
		method                         string
		google, password, vendor, move bool
		auto                           bool
	}
	for name, tc := range map[string]struct {
		st models.MailboxSigninState
		in signinInputs
		want
	}{
		"older sign-in, unknown scopes":  {google(models.MailAuthOAuth, nil, nil), on, want{models.SigninMethodGoogle, true, true, false, true, false}},
		"older sign-in through a vendor": {google(models.MailAuthOAuth, modify, zap), on, want{models.SigninMethodGoogle, true, true, true, true, true}},
		"full mail access":               {google(models.MailAuthOAuth, full, zap), on, want{models.SigninMethodGoogle, false, true, false, true, false}},
		"legacy grant through a vendor":  {google(models.MailAuthDelegated, nil, zap), on, want{models.SigninMethodGrant, false, true, true, false, true}},
		"automatic switch turned off":    {google(models.MailAuthDelegated, nil, zap), signinInputs{clientID: "client-1"}, want{models.SigninMethodGrant, false, true, true, false, false}},
		"no Google sign-in configured":   {google(models.MailAuthOAuth, nil, zap), signinInputs{grant: grant, automatic: true}, want{models.SigninMethodGoogle, false, true, false, true, false}},
		"personal Gmail": {func() models.MailboxSigninState {
			st := google(models.MailAuthOAuth, nil, zap)
			st.Email = avaAddr
			return st
		}(), signinInputs{clientID: "client-1", personal: true, automatic: true}, want{models.SigninMethodGoogle, false, true, false, false, false}},
		"a vendor that cannot sign in": {google(models.MailAuthOAuth, nil, &models.VendorConnection{ID: uuid.New(), Vendor: mailvendor.VendorMaildoso, Status: "active"}), on,
			want{models.SigninMethodGoogle, true, true, false, true, false}},
		"an invalid vendor key": {google(models.MailAuthOAuth, nil, &models.VendorConnection{ID: uuid.New(), Vendor: mailvendor.VendorZapmail, Status: "invalid"}), on,
			want{models.SigninMethodGoogle, true, true, false, true, false}},
		"app password through a vendor": {models.MailboxSigninState{Provider: models.InboxProviderSMTPIMAP, AuthMethod: "app_password", MailHost: "google_workspace",
			Status: "active", Vendor: zap, VendorMailboxID: "v1"}, on, want{models.SigninMethodAppPassword, false, false, true, false, false}},
		"switched off by its owner": {func() models.MailboxSigninState {
			st := google(models.MailAuthDelegated, nil, zap)
			st.Status = "inactive"
			return st
		}(), on, want{models.SigninMethodGrant, false, true, true, false, false}},
		"held by Warmbly Cloud": {func() models.MailboxSigninState {
			st := google(models.MailAuthOAuth, nil, zap)
			st.Managed = true
			return st
		}(), on, want{models.SigninMethodGoogle, false, false, false, false, false}},
		"an Outlook mailbox": {models.MailboxSigninState{Provider: models.InboxProviderOutlook, AuthMethod: models.MailAuthOAuth, Vendor: zap, VendorMailboxID: "v1"}, on,
			want{models.SigninMethodOther, false, false, false, false, false}},
	} {
		t.Run(name, func(t *testing.T) {
			got := decideSignin(tc.st, tc.in)
			if got.Method != tc.method || (got.Options.GoogleSignin != nil) != tc.google || got.Options.AppPassword != tc.password ||
				(got.Options.Vendor != nil) != tc.vendor || (got.Options.Grant != nil) != tc.move || got.AutoSwitch != tc.auto {
				t.Fatalf("got %+v options %+v, want %+v", got, got.Options, tc.want)
			}
			if got.Options.GoogleSignin != nil && got.Options.GoogleSignin.ClientID != "client-1" {
				t.Fatalf("client id = %q", got.Options.GoogleSignin.ClientID)
			}
		})
	}
}

func TestGroupSwitchCandidatesKeepsEachConnectionTogether(t *testing.T) {
	org, a, b := uuid.New(), uuid.New(), uuid.New()
	m1, m2, m3 := uuid.New(), uuid.New(), uuid.New()
	got := groupSwitchCandidates([]repository.SigninSwitchCandidate{
		{VendorReconnect: repository.VendorReconnect{AccountID: m1, OrgID: org, ConnectionID: a}},
		{VendorReconnect: repository.VendorReconnect{AccountID: m2, OrgID: org, ConnectionID: b}},
		{VendorReconnect: repository.VendorReconnect{AccountID: m3, OrgID: org, ConnectionID: a}},
	})
	if len(got) != 2 || got[0].conn != a || len(got[0].ids) != 2 || got[0].ids[1] != m3 || got[1].conn != b {
		t.Fatalf("groups = %+v", got)
	}
}
