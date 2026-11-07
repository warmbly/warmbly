package vendorconn

import (
	"context"
	"sync"
	"testing"
	"time"

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

// recordingAudit collects what reached the audit spine.
type recordingAudit struct {
	mu   sync.Mutex
	rows []auditRow
}

type auditRow struct {
	actor  uuid.UUID
	entity models.AuditEntityType
	id     uuid.UUID
	meta   map[string]string
}

func (r *recordingAudit) LogAction(_ context.Context, _, actor uuid.UUID, _ models.AuditAction, entity models.AuditEntityType, id *uuid.UUID, _, _ string, _, meta map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, auditRow{actor: actor, entity: entity, id: *id, meta: meta})
}

func (r *recordingAudit) outcomes() map[uuid.UUID]auditRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[uuid.UUID]auditRow{}
	for _, row := range r.rows {
		if row.entity == models.AuditEntityEmailAccount {
			out[row.id] = row
		}
	}
	return out
}

func TestAutoSwitchStartsOnceAndRecordsEveryOutcome(t *testing.T) {
	client := &consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{
		{ID: "ws:m1", Email: avaAddr, Domain: "acme.io", Admin: true}, {ID: "ws:m2", Email: leoAddr, Domain: "acme.io"},
	}}, trustState: mailvendor.AuthorizationCompleted}
	s, links, org, conn := consentFixture(t, client)
	s.kickReconnect = nil
	ctx := context.Background()
	repo := s.repo.(*memVendors)
	audit := &recordingAudit{}
	enabled := false
	s.WireSigninSwitch(audit, func(context.Context) bool { return enabled })
	owner, ava, leo, gone := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo.linked = []repository.VendorReconnect{
		{AccountID: ava, OrgID: org, ConnectionID: conn.ID, VendorMailboxID: "ws:m1", Email: avaAddr},
		{AccountID: leo, OrgID: org, ConnectionID: conn.ID, VendorMailboxID: "ws:m2", Email: leoAddr},
	}
	for _, id := range []uuid.UUID{ava, leo, gone} {
		repo.cands = append(repo.cands, repository.SigninSwitchCandidate{OwnerID: owner,
			VendorReconnect: repository.VendorReconnect{AccountID: id, OrgID: org, ConnectionID: conn.ID}})
	}
	key := reconnectJobKey(org, conn.ID)
	t.Cleanup(func() {
		_ = s.cache.Del(ctx, key).Err()
		_ = s.cache.SRem(ctx, reconnectJobsIndex, key).Err()
	})

	if err := s.runAutoSwitch(ctx); err != nil || len(repo.switches) != 0 || len(links.minted) != 0 {
		t.Fatalf("the switch ran while turned off: %v, %+v", err, repo.switches)
	}

	enabled = true
	if err := s.runAutoSwitch(ctx); err != nil {
		t.Fatal(err)
	}
	if sw := repo.switches[ava]; sw == nil || sw.Status != ReconnectQueued || !sw.Automatic {
		t.Fatalf("ava after start = %+v", sw)
	}
	// A candidate the vendor account no longer lists is failed with a reason, never left queued.
	if sw := repo.switches[gone]; sw == nil || sw.Status != ReconnectFailed || sw.Message == "" {
		t.Fatalf("unlisted mailbox = %+v", sw)
	}

	// A second run finds them claimed and starts nothing.
	if err := s.runAutoSwitch(ctx); err != nil {
		t.Fatal(err)
	}
	s.driveReconnectKey(ctx, key)
	s.driveReconnectKey(ctx, key)
	if client.consents != 2 || len(links.minted) != 2 {
		t.Fatalf("%d consent requests, %d links: a mailbox was asked for twice", client.consents, len(links.minted))
	}
	for _, m := range links.minted {
		if m.UserID != owner {
			t.Fatalf("sign-in minted for %s, want the workspace owner", m.UserID)
		}
	}
	if sw := repo.switches[leo]; sw.Status != ReconnectSigningIn {
		t.Fatalf("leo while signing in = %+v", sw)
	}

	s.ConsentSettled(ctx, org, conn.ID, avaAddr, "")
	s.ConsentSettled(ctx, org, conn.ID, leoAddr, "a login challenge")
	s.driveReconnectKey(ctx, key)
	if repo.switches[ava].Status != ReconnectConnected || repo.switches[leo].Status != ReconnectFailed || repo.switches[leo].Message == "" {
		t.Fatalf("final = ava %+v, leo %+v", repo.switches[ava], repo.switches[leo])
	}
	out := audit.outcomes()
	for _, id := range []uuid.UUID{ava, leo, gone} {
		row, ok := out[id]
		if !ok || row.actor != uuid.Nil || row.meta["automatic"] != "true" || row.meta["action"] != "signin_switch" {
			t.Fatalf("outcome for %s = %+v, %v", id, row, ok)
		}
	}

	// A week on, a failure is tried again; a connected mailbox never is.
	repo.switches[leo].At = time.Now().Add(-repository.SigninSwitchRetry - time.Minute)
	repo.switches[ava].At = repo.switches[leo].At
	repo.cands = repo.cands[:2]
	if err := s.runAutoSwitch(ctx); err != nil {
		t.Fatal(err)
	}
	if repo.switches[leo].Status != ReconnectQueued || repo.switches[ava].Status != ReconnectConnected {
		t.Fatalf("after a week = ava %+v, leo %+v", repo.switches[ava], repo.switches[leo])
	}
}

func TestManualReconnectRecordsItsStart(t *testing.T) {
	client := &consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{{ID: "ws:m1", Email: avaAddr, Domain: "acme.io", Admin: true}}},
		trustState: mailvendor.AuthorizationPending}
	s, _, org, conn := consentFixture(t, client)
	s.kickReconnect = nil
	ctx := context.Background()
	repo := s.repo.(*memVendors)
	audit := &recordingAudit{}
	s.WireSigninSwitch(audit, nil)
	ava := uuid.New()
	repo.linked = []repository.VendorReconnect{{AccountID: ava, OrgID: org, ConnectionID: conn.ID, VendorMailboxID: "ws:m1", Email: avaAddr}}
	key := reconnectJobKey(org, conn.ID)
	t.Cleanup(func() {
		_ = s.cache.Del(ctx, key).Err()
		_ = s.cache.SRem(ctx, reconnectJobsIndex, key).Err()
	})

	member := uuid.New()
	if _, xerr := s.Reconnect(ctx, org, member, conn.ID, &ava); xerr != nil {
		t.Fatal(xerr)
	}
	if sw := repo.switches[ava]; sw == nil || sw.Status != ReconnectQueued || sw.Automatic {
		t.Fatalf("after the click = %+v", sw)
	}
	s.driveReconnectKey(ctx, key)
	if sw := repo.switches[ava]; sw.Status != ReconnectTrusting {
		t.Fatalf("while trusting = %+v", sw)
	}
	// Progress is one event for the connection, not one per mailbox.
	audit.mu.Lock()
	n := len(audit.rows)
	last := audit.rows[n-1]
	audit.mu.Unlock()
	if n != 1 || last.entity != models.AuditEntityMailboxVendor || last.meta["action"] != "reconnect_progress" {
		t.Fatalf("audit = %+v", audit.rows)
	}
}

func TestReconnectMailboxesTakesOnlyTheNamedOnes(t *testing.T) {
	client := &consentClient{trustState: mailvendor.AuthorizationPending}
	s, _, org, conn := consentFixture(t, client)
	s.kickReconnect = nil
	ctx := context.Background()
	repo := s.repo.(*memVendors)
	ava, leo, other := uuid.New(), uuid.New(), uuid.New()
	repo.linked = []repository.VendorReconnect{
		{AccountID: ava, OrgID: org, ConnectionID: conn.ID, VendorMailboxID: "ws:m1", Email: avaAddr},
		{AccountID: leo, OrgID: org, ConnectionID: conn.ID, VendorMailboxID: "ws:m2", Email: leoAddr},
	}
	key := reconnectJobKey(org, conn.ID)
	t.Cleanup(func() {
		_ = s.cache.Del(ctx, key).Err()
		_ = s.cache.SRem(ctx, reconnectJobsIndex, key).Err()
	})
	out, xerr := s.ReconnectMailboxes(ctx, org, uuid.New(), conn.ID, []uuid.UUID{leo, other})
	if xerr != nil || len(out) != 1 || out[0].EmailAccountID != leo {
		t.Fatalf("named = %+v, %v", out, xerr)
	}
	if _, started := repo.switches[ava]; started {
		t.Fatal("a mailbox nobody named was started")
	}
}
