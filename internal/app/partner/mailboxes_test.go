package partner

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestEnablementLanes(t *testing.T) {
	for e, want := range map[models.PartnerEnablement][2]bool{
		models.PartnerEnablementOff:           {false, false},
		models.PartnerEnablementWarmupOnly:    {false, true},
		models.PartnerEnablementSendingWarmup: {true, true},
		"":                                    {false, false},
	} {
		if e.Campaigns() != want[0] || e.Warmup() != want[1] {
			t.Errorf("%q: campaigns=%v warmup=%v, want %v", e, e.Campaigns(), e.Warmup(), want)
		}
	}
}

// A return URL is the partner's own host over https, never a path, another host or credentials.
func TestCleanReturnURLHosts(t *testing.T) {
	hosts := []string{"app.partner.test", "api.partner.test"}
	for raw, ok := range map[string]bool{
		"https://app.partner.test/w/1/integrations?x=1": true,
		"https://API.partner.test/cb":                   true,
		"https://evil.test/cb":                          false,
		"https://app.partner.test.evil.test/":           false,
		"https://user:pw@app.partner.test/":             false,
		"/app/emails":                                   false,
		"":                                              false,
		"javascript:alert(1)":                           false,
		"//app.partner.test/x":                          false,
	} {
		if _, got := CleanReturnURL(raw, hosts); got != ok {
			t.Errorf("%q accepted=%v, want %v", raw, got, ok)
		}
	}
}

func TestValidateAttach(t *testing.T) {
	creds := &MailboxCredentials{Username: "ada", Password: "pw", IMAP: Endpoint{Host: "imap.acme.test", Port: 993, Security: "ssl"},
		SMTP: Endpoint{Host: "smtp.acme.test", Port: 587, Security: "starttls"}}
	ok := []AttachInput{
		{Source: "partner", Email: "Ada@Acme.test", Provider: "smtp", Enablement: "sending_warmup", Connection: AttachConnection{Method: "password", Credentials: creds}},
		{Source: "graphkit_infrastructure", Email: "ada@acme.test", Provider: "google", Enablement: "warmup_only", Connection: AttachConnection{Method: "token"}},
		{Email: "ada@acme.test", Provider: "microsoft", Connection: AttachConnection{Method: "delegation"}},
	}
	for i, in := range ok {
		if xerr := validateAttach("omb_1", &in); xerr != nil {
			t.Fatalf("%d: %v", i, xerr)
		}
		if in.Email != "ada@acme.test" || in.PartnerMailboxID != "omb_1" || in.Enablement == "" {
			t.Fatalf("%d: normalized to %+v", i, in)
		}
	}
	bad := []AttachInput{
		{Source: "graphkit_infrastructure", Email: "ada@acme.test", Provider: "smtp", Connection: AttachConnection{Method: "password", Credentials: creds}},
		{Source: "partner", Email: "ada@acme.test", Provider: "google", Connection: AttachConnection{Method: "token"}},
		{Source: "partner", Email: "ada@acme.test", Provider: "smtp", Connection: AttachConnection{Method: "password"}},
		{Source: "partner", Email: "not an address", Provider: "smtp", Connection: AttachConnection{Method: "password", Credentials: creds}},
		{Source: "partner", Email: "ada@acme.test", Provider: "smtp", Enablement: "on", Connection: AttachConnection{Method: "password", Credentials: creds}},
		{Source: "manual", Email: "ada@acme.test", Provider: "google", Connection: AttachConnection{Method: "token"}},
		{PartnerMailboxID: "omb_2", Email: "ada@acme.test", Provider: "google", Connection: AttachConnection{Method: "token"}},
	}
	for i, in := range bad {
		if xerr := validateAttach("omb_1", &in); xerr == nil {
			t.Errorf("%d: accepted %+v", i, in)
		}
	}
}

func TestBuildStatementPricesLiveDaysOnly(t *testing.T) {
	p := &models.Partner{ID: uuid.New(), WholesaleSendingWarmupDay: 70_000, WholesaleWarmupOnlyDay: 50_000}
	rows := []models.PartnerMailboxDays{
		{OrgExternalID: "b", SendingWarmupDays: 10, WarmupOnlyDays: 2},
		{OrgExternalID: "a", SendingWarmupDays: 3},
		{OrgExternalID: "b", WarmupOnlyDays: 5},
	}
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	st := buildStatement(p, models.PartnerModeLive, month, rows)
	if st.Month != "2026-10" || len(st.Lines) != 2 || st.Lines[0].OrgExternalID != "a" {
		t.Fatalf("statement = %+v", st)
	}
	if st.SendingWarmupDays != 13 || st.WarmupOnlyDays != 7 || st.AmountMicros != 13*70_000+7*50_000 || st.Lines[1].AmountMicros != 10*70_000+7*50_000 {
		t.Fatalf("totals = %+v", st)
	}
	if test := buildStatement(p, models.PartnerModeTest, month, rows); test.AmountMicros != 0 || test.SendingWarmupDays != 13 {
		t.Fatalf("test mode billed %d", test.AmountMicros)
	}
}

func TestParseMonth(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	if m, xerr := ParseMonth("", now); xerr != nil || !m.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("default month = %v %v", m, xerr)
	}
	for _, bad := range []string{"2026-13", "2026-1", "november", "2026-11-01"} {
		if _, xerr := ParseMonth(bad, now); xerr == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// A mailbox waiting on its tenant's consent says which step and why; one with nothing to do keeps its old fingerprint.
func TestMailboxViewCarriesActionRequired(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	msg := "Approve Warmbly in Microsoft 365 for getacme.com with the domain's admin mailbox."
	action := models.ActionMicrosoftConsent
	v := &repository.PartnerMailboxView{Status: "connecting", WarmupState: "off", ActionRequired: &action,
		Mailbox: models.PartnerMailbox{ID: uuid.New(), Email: "ada@getacme.com", Provider: "microsoft", Source: models.PartnerSourceInfrastructure,
			Enablement: models.PartnerEnablementOff, ConnectError: &msg}}
	got := mailboxView(v, 50, now)
	if got.ActionRequired == nil || *got.ActionRequired != "microsoft_consent" || got.StatusError == nil || *got.StatusError != msg || got.Status != "connecting" {
		t.Fatalf("view = %+v", got)
	}
	if publishedState(v) != "connecting|off|off|microsoft_consent" {
		t.Fatalf("fingerprint = %q", publishedState(v))
	}
	v.ActionRequired = nil
	got = mailboxView(v, 50, now)
	if got.ActionRequired != nil || got.StatusError != nil {
		t.Fatalf("view without a step = %+v", got)
	}
	if publishedState(v) != "connecting|off|off" {
		t.Fatalf("fingerprint without a step = %q", publishedState(v))
	}
}
