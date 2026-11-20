package partner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// fakeConsents records the tenant consents a partner started; consented answers that none is needed.
type fakeConsents struct {
	started   []string
	consented bool
}

func (f *fakeConsents) StartPurchasedMicrosoftConsent(_ context.Context, orgID, _ uuid.UUID, domain, returnURL string) (*delegation.MicrosoftConsent, *errx.Error) {
	f.started = append(f.started, orgID.String()+" "+domain+" "+returnURL)
	if f.consented {
		return &delegation.MicrosoftConsent{Consented: true}, nil
	}
	return &delegation.MicrosoftConsent{URL: "https://login.example/organizations/v2.0/adminconsent?state=mpc_x", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func microsoftAttach(email string) AttachInput {
	return AttachInput{Source: models.PartnerSourceInfrastructure, Email: email, Provider: "microsoft", Enablement: "warmup_only",
		Connection: AttachConnection{Method: "delegation"}}
}

// An infrastructure Microsoft 365 mailbox connects through this instance's own app once its tenant's administrator
// consented for the workspace. Until then it waits, announced with action_required, without spending its attempts.
func TestLivePartnerMicrosoftMailboxWaitsForTenantConsent(t *testing.T) {
	f := liveSetup(t)
	del := &fakeDelegator{delegated: map[string]bool{}}
	consents := &fakeConsents{}
	f.svc.WireMailboxes(MailboxDeps{Repo: repository.NewPartnerMailboxRepository(f.pool), Delegator: del, Consents: consents})
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM partner_mailboxes WHERE partner_id = $1`, f.a.ID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM partner_events WHERE partner_id = $1`, f.a.ID)
	})
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	if _, _, xerr := f.svc.Upsert(ctx, c, "ws-ms", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	// The attach answer already says what the mailbox waits on.
	m, _, xerr := f.svc.Attach(ctx, c, "ws-ms", "omb_ms", microsoftAttach("ada@getacme.test"))
	if xerr != nil || m.Status != "connecting" || m.ActionRequired == nil || *m.ActionRequired != models.ActionMicrosoftConsent ||
		m.StatusError == nil || *m.StatusError != delegation.TenantConsentMessage("getacme.test") {
		t.Fatalf("attach = %+v %v", m, xerr)
	}
	due := func() {
		t.Helper()
		if _, err := f.pool.Exec(ctx, `UPDATE partner_mailboxes SET next_connect_at = now() WHERE partner_id = $1 AND connect_state = 'pending'`, f.a.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.ConnectDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	read := func() Mailbox {
		t.Helper()
		page, xerr := f.svc.ListMailboxes(ctx, c, "ws-ms", "", 100)
		if xerr != nil || len(page.Data) != 1 {
			t.Fatalf("list = %+v %v", page, xerr)
		}
		return page.Data[0]
	}

	// No workspace grant covers the domain: partner input alone never records one, so the mailbox waits.
	for range connectMaxAttempts + 2 {
		due()
	}
	got := read()
	if got.Status != "connecting" || got.ActionRequired == nil || *got.ActionRequired != models.ActionMicrosoftConsent ||
		got.StatusError == nil || *got.StatusError != delegation.TenantConsentMessage("getacme.test") {
		t.Fatalf("waiting mailbox = %+v", got)
	}
	if len(del.microsoft) != 0 || len(del.delegated) != 0 {
		t.Fatalf("connected without consent: %v %v", del.microsoft, del.delegated)
	}
	var attempts int
	var next time.Time
	if err := f.pool.QueryRow(ctx, `SELECT connect_attempts, next_connect_at FROM partner_mailboxes WHERE partner_id = $1`, f.a.ID).Scan(&attempts, &next); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || time.Until(next) < 20*time.Minute {
		t.Fatalf("attempts=%d next in %s, want no attempt spent and a slow retry", attempts, time.Until(next))
	}

	// The wait is announced once as mailbox.updated, carrying the step.
	if err := f.svc.SyncMailboxes(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SyncMailboxes(ctx); err != nil {
		t.Fatal(err)
	}
	events, xerr := f.svc.Events(ctx, c, "", 100)
	if xerr != nil {
		t.Fatal(xerr)
	}
	updates := 0
	for _, e := range events.Data {
		if e.Type != models.PartnerEventMailboxUpdated {
			continue
		}
		updates++
		var body Mailbox
		if err := json.Unmarshal(e.Data, &body); err != nil || body.ActionRequired == nil || *body.ActionRequired != models.ActionMicrosoftConsent {
			t.Fatalf("mailbox.updated = %s %v", e.Data, err)
		}
	}
	if updates != 1 {
		t.Fatalf("mailbox.updated sent %d times, want once", updates)
	}

	// A consent link answers for an attached Microsoft domain on an allowed host only.
	link, xerr := f.svc.CreateMicrosoftConsentLink(ctx, c, "ws-ms", MicrosoftConsentInput{Domain: "GetAcme.test", ReturnURL: "https://app.partner.test/w/1?tab=mail"})
	if xerr != nil || link.URL == nil || link.ExpiresAt == nil || link.Consented {
		t.Fatalf("link = %+v %v", link, xerr)
	}
	if len(consents.started) != 1 || !strings.HasSuffix(consents.started[0], " getacme.test https://app.partner.test/w/1?tab=mail") {
		t.Fatalf("started = %v", consents.started)
	}
	if _, xerr := f.svc.CreateMicrosoftConsentLink(ctx, c, "ws-ms", MicrosoftConsentInput{Domain: "getacme.test", ReturnURL: "https://evil.test/cb"}); xerr == nil || xerr.Identifier != "invalid_request" {
		t.Fatalf("foreign return_url = %v", xerr)
	}
	if _, xerr := f.svc.CreateMicrosoftConsentLink(ctx, c, "ws-ms", MicrosoftConsentInput{Domain: "victim.test", ReturnURL: "https://app.partner.test/cb"}); xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("unattached domain = %v", xerr)
	}
	if _, xerr := f.svc.CreateMicrosoftConsentLink(ctx, f.caller(t, f.b, models.PartnerModeLive), "ws-ms", MicrosoftConsentInput{Domain: "getacme.test", ReturnURL: "https://app.partner.test/cb"}); xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("another partner's workspace = %v", xerr)
	}
	consents.consented = true
	if link, xerr := f.svc.CreateMicrosoftConsentLink(ctx, c, "ws-ms", MicrosoftConsentInput{Domain: "getacme.test", ReturnURL: "https://app.partner.test/cb"}); xerr != nil || link.URL != nil || !link.Consented {
		t.Fatalf("consented link = %+v %v", link, xerr)
	}

	// The administrator consented: the grant exists, the wake makes the mailbox due now and it connects.
	del.proven = "getacme.test"
	po, err := f.svc.Repo.GetPartnerOrg(ctx, f.a.ID, models.PartnerModeLive, "ws-ms")
	if err != nil || po == nil {
		t.Fatal(err)
	}
	f.svc.WakeMicrosoftConsent(ctx, *po.OrganizationID, []string{"getacme.test"})
	if err := f.svc.ConnectDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(del.microsoft) != 1 || del.microsoft[0] != "ada@getacme.test" {
		t.Fatalf("microsoft connects = %v, want the mailbox through this instance's app", del.microsoft)
	}

	// A tenant whose consent was withdrawn waits again rather than failing.
	if _, _, xerr := f.svc.Attach(ctx, c, "ws-ms", "omb_ms2", microsoftAttach("bob@getacme.test")); xerr != nil {
		t.Fatal(xerr)
	}
	del.consentMissing = true
	due()
	var action *string
	if err := f.pool.QueryRow(ctx, `SELECT action_required FROM partner_mailboxes WHERE partner_id = $1 AND email = 'bob@getacme.test'`, f.a.ID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action == nil || *action != models.ActionMicrosoftConsent {
		t.Fatalf("withdrawn consent action = %v", action)
	}
}
