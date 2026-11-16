package toolmigrate

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type fakeMailboxImports struct{ inputs []mailboximport.CreateInput }

func (f *fakeMailboxImports) Create(_ context.Context, c mailboximport.CreateInput) (*models.MailboxImport, *errx.Error) {
	f.inputs = append(f.inputs, c)
	return &models.MailboxImport{ID: uuid.New()}, nil
}

type fakeAgencies struct {
	role    models.AgencyRole
	created []string
	// members, when set, makes the caller a member of every client it creates.
	members fakeMembers
}

func (f *fakeAgencies) Get(context.Context, uuid.UUID, uuid.UUID) (*models.Agency, *errx.Error) {
	return &models.Agency{Role: f.role}, nil
}

func (f *fakeAgencies) CreateClient(_ context.Context, _, userID uuid.UUID, name, _ string) (*models.AgencyClient, *errx.Error) {
	f.created = append(f.created, name)
	if name == "fail" {
		return nil, errx.New(errx.BadRequest, "that name is taken")
	}
	org := uuid.New()
	if f.members != nil {
		now := time.Now()
		f.members[org] = &models.OrganizationMember{OrganizationID: org, UserID: userID, Role: "owner", Permissions: models.AllPermissions, AcceptedAt: &now}
	}
	return &models.AgencyClient{OrganizationID: org}, nil
}

type fakeAgencyClients []models.AgencyClientMembership

func (f fakeAgencyClients) ClientMemberships(context.Context, uuid.UUID, uuid.UUID) ([]models.AgencyClientMembership, error) {
	return f, nil
}

type fixture struct {
	svc       *Service
	repo      *memRepo
	provider  *fakeProvider
	contacts  *fakeContacts
	campaigns *fakeCampaigns
	outreach  *fakeOutreach
	mailboxes *fakeMailboxImports
	members   fakeMembers
	org       uuid.UUID
	user      uuid.UUID
}

func newFixture() *fixture {
	f := &fixture{
		repo: newMemRepo(), contacts: &fakeContacts{}, campaigns: &fakeCampaigns{}, outreach: &fakeOutreach{},
		mailboxes: &fakeMailboxImports{}, org: uuid.New(), user: uuid.New(),
	}
	f.provider = &fakeProvider{info: source.Info{
		ID: "faketool", Label: "FakeTool", Kinds: source.AllKinds, MailboxSecrets: true,
		Fields: []source.Field{{Key: "api_key", Label: "API key", Secret: true}},
	}}
	now := time.Now()
	f.members = fakeMembers{f.org: {OrganizationID: f.org, UserID: f.user, Role: "admin", Permissions: models.AllPermissions, AcceptedAt: &now}}
	f.svc = NewService(Deps{
		Repo: f.repo, Registry: source.NewRegistry(f.provider), Sealer: plainSealer{},
		Contacts: f.contacts, Campaigns: f.campaigns, Steps: fakeSteps{}, Outreach: f.outreach,
		Categories: fakeCategories{}, Members: f.members, Mailboxes: f.mailboxes,
	})
	return f
}

func (f *fixture) scope() Scope { return Scope{OrgID: &f.org} }

func (f *fixture) actor() Actor {
	return Actor{UserID: f.user, OrgPerms: models.AllPermissions, ViaSession: true}
}

// ready creates a migration and runs its scan.
func (f *fixture) ready(t *testing.T) *models.ToolMigration {
	t.Helper()
	m, xerr := f.svc.Create(context.Background(), f.scope(), f.actor(), models.NewToolMigration{
		Provider: "faketool", Credentials: []source.Credentials{{"api_key": " good-key "}},
	})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}
	f.svc.pass(context.Background())
	m, xerr = f.svc.Get(context.Background(), f.scope(), m.ID)
	if xerr != nil || m.Status != models.ToolMigrationReady {
		t.Fatalf("after scan: %+v %v", m, xerr)
	}
	return m
}

func TestCreateRefusesARejectedKeyAndStoresNothing(t *testing.T) {
	f := newFixture()
	_, xerr := f.svc.Create(context.Background(), f.scope(), f.actor(), models.NewToolMigration{
		Provider: "faketool", Credentials: []source.Credentials{{"api_key": "bad"}},
	})
	if xerr == nil || xerr.Identifier != CodeKeyRejected || xerr.Code != errx.Unprocessable {
		t.Fatalf("got %v; want 422 %s", xerr, CodeKeyRejected)
	}
	if len(f.repo.migs) != 0 {
		t.Fatal("a rejected key was stored")
	}
	if _, xerr := f.svc.Create(context.Background(), f.scope(), f.actor(), models.NewToolMigration{Provider: "nope"}); xerr == nil || xerr.Identifier != CodeProviderUnknown {
		t.Fatalf("unknown provider: %v", xerr)
	}
}

func TestCreateSealsTheKeyAndScans(t *testing.T) {
	f := newFixture()
	m := f.ready(t)
	stored := f.repo.migs[m.ID]
	if stored.sealed == nil || strings.Contains(*stored.sealed, "good-key") {
		t.Fatalf("credentials stored unsealed: %v", stored.sealed)
	}
	if !m.CredentialsHeld || len(m.Accounts) != 1 || len(m.Inventory) != 1 || m.Inventory[0].Inventory.Leads != -1 {
		t.Fatalf("scan result: %+v", m)
	}
	if m.Label != "FakeTool" {
		t.Fatalf("label = %q; want the provider's", m.Label)
	}
	// Another workspace reads it as not found.
	other := uuid.New()
	if _, xerr := f.svc.Get(context.Background(), Scope{OrgID: &other}, m.ID); xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("cross-workspace get: %v", xerr)
	}
}

func TestStartRefusesContinueAndMissingPermissions(t *testing.T) {
	f := newFixture()
	m := f.ready(t)
	ctx := context.Background()

	_, xerr := f.svc.Start(ctx, f.scope(), f.actor(), m.ID, models.StartToolMigration{Options: models.ToolMigrationOptions{
		Kinds: []source.Kind{source.KindLeads}, InFlight: models.ToolMigrationContinue,
	}})
	if xerr == nil || xerr.Identifier != CodeInFlightUnsupported || xerr.Code != errx.BadRequest {
		t.Fatalf("continue: %v", xerr)
	}

	noCampaigns := f.actor()
	noCampaigns.OrgPerms &^= models.PermManageCampaigns
	_, xerr = f.svc.Start(ctx, f.scope(), noCampaigns, m.ID, models.StartToolMigration{Options: models.ToolMigrationOptions{
		Kinds: []source.Kind{source.KindCampaigns, source.KindLeads},
	}})
	if xerr == nil || xerr.Identifier != CodePermission {
		t.Fatalf("campaigns without permission: %v", xerr)
	}
	if got, _ := f.svc.Get(ctx, f.scope(), m.ID); got.Status != models.ToolMigrationReady {
		t.Fatalf("a refused start changed the migration: %s", got.Status)
	}
}

// richSource is a source account with one of everything the runner handles.
func richSource(p *fakeProvider) {
	yes := true
	p.blocklist = []source.BlockEntry{{Value: "spam@blocked.com"}, {Value: "blocked.org", Domain: true}}
	p.mailboxes = []source.Mailbox{
		{Email: "Sender@Acme.io", Kind: source.MailboxSMTP, Warmup: &yes,
			SMTP: &source.Server{Host: "smtp.acme.io", Port: 465, Security: source.SecuritySSL, Password: "smtp-secret"},
			IMAP: &source.Server{Host: "imap.acme.io", Port: 993, Security: source.SecuritySSL, Password: "imap-secret"}},
		{Email: "google@acme.io", Kind: source.MailboxGoogle},
		{Email: "who@acme.io", Kind: "unknown"},
	}
	p.campaigns = []source.Campaign{
		{ID: "c1", Name: "Q3 outreach", Status: source.CampaignActive, Senders: []string{"linked@acme.io", "missing@acme.io"},
			StopOnReply: true, TrackOpens: true, MaxNewLeadsPerDay: 25,
			Schedule: &source.Schedule{Timezone: "Europe/London", Days: []int{1, 2, 3, 4, 5}, From: "09:00", To: "17:00"},
			Steps: []source.Step{
				{Variants: []source.Variant{{Subject: "Hi {{.FirstName}}", Body: "Hello there"}, {Subject: "Hey", Body: "Second take"}}},
				{WaitDays: 2, WaitMinutes: 30, Variants: []source.Variant{{Body: "Following up"}}},
			}},
		{ID: "c2", Name: "Old", Status: source.CampaignCompleted, Steps: []source.Step{{Variants: []source.Variant{{Subject: "x", Body: "y"}}}}},
	}
	p.leads = map[string][]source.Lead{
		"c1": {
			{Email: "New@Lead.com", Status: source.LeadNotStarted, Title: "CEO", Custom: map[string]string{"title": "Chief", "Team Size": "12"}},
			{Email: "replied@lead.com", Status: source.LeadReplied, StepsSent: 1},
			{Email: "unsub@lead.com", Status: source.LeadUnsubscribed},
			{Email: "midway@lead.com", Status: source.LeadInProgress, StepsSent: 1},
			{Email: "bounce@lead.com", Status: source.LeadBounced},
			{Email: "not-an-address", Status: source.LeadNotStarted},
		},
		// c2 has no per-campaign listing: unsupported, which is not a failure.
		"": {{Email: "new@lead.com"}, {Email: "loose@lead.com", Website: "lead.com"}},
	}
}

func (f *fixture) start(t *testing.T, m *models.ToolMigration, opts models.ToolMigrationOptions) {
	t.Helper()
	if _, xerr := f.svc.Start(context.Background(), f.scope(), f.actor(), m.ID, models.StartToolMigration{Options: opts}); xerr != nil {
		t.Fatalf("start: %v", xerr)
	}
	f.svc.pass(context.Background())
}

func TestRunBringsEverythingOver(t *testing.T) {
	f := newFixture()
	richSource(f.provider)
	linked := uuid.New()
	f.repo.mailboxes["linked@acme.io"] = linked
	m := f.ready(t)
	f.start(t, m, models.ToolMigrationOptions{Kinds: source.AllKinds, SkipFinished: true, InFlight: models.ToolMigrationSkip, Warmup: true})

	m, _ = f.svc.Get(context.Background(), f.scope(), m.ID)
	if m.Status != models.ToolMigrationDone || m.CredentialsHeld {
		t.Fatalf("migration %s held=%v; want completed with keys wiped", m.Status, m.CredentialsHeld)
	}
	tg := m.Targets[0]
	if tg.Status != models.ToolMigrationTargetDone {
		t.Fatalf("target %s: %s", tg.Status, tg.Error)
	}

	// Blocklist and opted-out leads are suppressed; nobody who opted out is enrolled.
	for _, want := range []string{"spam@blocked.com", "blocked.org", "unsub@lead.com", "bounce@lead.com"} {
		if !slices.Contains(f.outreach.suppressed, want) {
			t.Errorf("%s not suppressed (have %v)", want, f.outreach.suppressed)
		}
	}

	// One draft campaign: the finished one is skipped, senders resolved, waits rounded up.
	if len(f.campaigns.created) != 1 {
		t.Fatalf("created %d campaigns; want 1", len(f.campaigns.created))
	}
	c := f.campaigns.created[0]
	if c.SenderStrategy == nil || *c.SenderStrategy != "explicit" || len(c.Senders) != 1 || c.Senders[0].EmailAccountID != linked {
		t.Errorf("senders: %+v", c.Senders)
	}
	if len(c.Sequences) != 2 || *c.Sequences[0].WaitAfter != 3 || !*c.Sequences[1].ThreadReply || *c.Sequences[0].ThreadReply {
		t.Errorf("steps: %+v", c.Sequences)
	}
	if c.MaxNewLeadsPerDay == nil || *c.MaxNewLeadsPerDay != 25 || c.Days == nil || *c.Days != 0b11111 || *c.Timezone != "Europe/London" {
		t.Errorf("settings: %+v", c)
	}
	if f.outreach.variants != 1 {
		t.Errorf("variants created = %d; want 1", f.outreach.variants)
	}
	if len(tg.Followup.Campaigns) != 1 || !reflect.DeepEqual(tg.Followup.Campaigns[0].MissingSenders, []string{"missing@acme.io"}) {
		t.Errorf("followup campaigns: %+v", tg.Followup.Campaigns)
	}
	if !hasNote(tg.Followup.Notes, "rounded up") {
		t.Errorf("no rounding note in %v", tg.Followup.Notes)
	}

	// Only the lead that had not started is enrolled; the account-level copy of it is not imported twice.
	if got := f.contacts.enrolled(); !reflect.DeepEqual(got, []string{"new@lead.com"}) {
		t.Errorf("enrolled %v", got)
	}
	imported := map[string]int{}
	for _, call := range f.contacts.calls {
		for _, e := range call.emails {
			imported[e]++
		}
	}
	if imported["new@lead.com"] != 1 || imported["loose@lead.com"] != 1 || imported["midway@lead.com"] != 1 {
		t.Errorf("imports: %v", imported)
	}
	custom := map[string]string{}
	for _, call := range f.contacts.calls {
		for k, v := range call.custom {
			custom[k] = v
		}
	}
	if custom["new@lead.com.title"] != "Chief" || custom["new@lead.com.team_size"] != "12" || custom["loose@lead.com.website"] != "lead.com" {
		t.Errorf("custom fields: %v", custom)
	}
	if tg.Counts.LeadsFailed != 1 || tg.Counts.LeadsEnrolled != 1 || tg.Counts.LeadsNotEnrolled != 2 || tg.Counts.CampaignsCreated != 1 {
		t.Errorf("counts: %+v", tg.Counts)
	}

	// The SMTP mailbox connects through the importer with its passwords; the rest wait for their owner.
	if len(f.mailboxes.inputs) != 1 || !strings.Contains(f.mailboxes.inputs[0].Input.Text, "smtp-secret") ||
		!strings.Contains(f.mailboxes.inputs[0].Input.Text, "sender@acme.io") {
		t.Fatalf("mailbox import: %+v", f.mailboxes.inputs)
	}
	var reconnect []string
	for _, r := range tg.Followup.Reconnect {
		reconnect = append(reconnect, r.Email)
	}
	if !reflect.DeepEqual(reconnect, []string{"google@acme.io", "who@acme.io"}) {
		t.Errorf("reconnect: %v", reconnect)
	}
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "good-key") {
		t.Fatal("a credential reached the API shape")
	}
}

func TestKeyStartedMigrationNeverUsesMailboxPasswords(t *testing.T) {
	f := newFixture()
	richSource(f.provider)
	m, xerr := f.svc.Create(context.Background(), f.scope(), Actor{UserID: f.user, OrgPerms: models.AllPermissions}, models.NewToolMigration{
		Provider: "faketool", Credentials: []source.Credentials{{"api_key": "good"}},
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	f.svc.pass(context.Background())
	f.start(t, m, models.ToolMigrationOptions{Kinds: []source.Kind{source.KindMailboxes}})
	if len(f.mailboxes.inputs) != 0 {
		t.Fatal("a key-started migration connected mailboxes with their passwords")
	}
	m, _ = f.svc.Get(context.Background(), f.scope(), m.ID)
	if n := len(m.Targets[0].Followup.Reconnect); n != 3 {
		t.Fatalf("reconnect = %d; want every mailbox", n)
	}
}

func TestResumedRunCreatesNothingTwice(t *testing.T) {
	f := newFixture()
	richSource(f.provider)
	m := f.ready(t)
	f.start(t, m, models.ToolMigrationOptions{Kinds: []source.Kind{source.KindCampaigns, source.KindMailboxes}, SkipFinished: true})

	// Replay the target from the start of its first phase, as a lapsed lease would.
	for _, tg := range f.repo.targets {
		tg.t.Status, tg.cursor = models.ToolMigrationTargetPending, nil
	}
	f.repo.migs[m.ID].m.Status = models.ToolMigrationRunning
	sealed := "sealed:" + reverse(`[{"api_key":"good-key"}]`)
	f.repo.migs[m.ID].sealed = &sealed
	f.svc.pass(context.Background())

	if len(f.campaigns.created) != 1 || len(f.mailboxes.inputs) != 1 || f.outreach.variants != 1 {
		t.Fatalf("replay wrote again: campaigns=%d mailbox imports=%d variants=%d",
			len(f.campaigns.created), len(f.mailboxes.inputs), f.outreach.variants)
	}
}

func TestTargetFailsWhenTheCreatorLosesPermission(t *testing.T) {
	f := newFixture()
	richSource(f.provider)
	m := f.ready(t)
	if _, xerr := f.svc.Start(context.Background(), f.scope(), f.actor(), m.ID, models.StartToolMigration{Options: models.ToolMigrationOptions{
		Kinds: []source.Kind{source.KindCampaigns},
	}}); xerr != nil {
		t.Fatal(xerr)
	}
	f.members[f.org].Permissions = models.PermManageContacts
	f.svc.pass(context.Background())

	m, _ = f.svc.Get(context.Background(), f.scope(), m.ID)
	if m.Targets[0].Status != models.ToolMigrationTargetFailed || len(f.campaigns.created) != 0 {
		t.Fatalf("target %s, campaigns %d; want failed with nothing written", m.Targets[0].Status, len(f.campaigns.created))
	}
	if m.Status != models.ToolMigrationFailed || m.CredentialsHeld {
		t.Fatalf("migration %s held=%v", m.Status, m.CredentialsHeld)
	}
}

func TestAgencyStartChecksEveryTargetBeforeCreatingAClient(t *testing.T) {
	f := newFixture()
	f.provider.workspace = []source.Workspace{{ID: "a", Name: "Client A"}, {ID: "b", Name: "Client B"}}
	agencyID := uuid.New()
	agencies := &fakeAgencies{role: models.AgencyRoleOwner}
	f.svc.d.Agencies = agencies
	f.svc.d.AgencyClients = fakeAgencyClients{{OrganizationID: f.org}}
	scope := Scope{AgencyID: &agencyID}
	actor := Actor{UserID: f.user, ViaSession: true}

	m, xerr := f.svc.Create(context.Background(), scope, actor, models.NewToolMigration{Provider: "faketool", Credentials: []source.Credentials{{"api_key": "k"}}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	f.svc.pass(context.Background())

	stranger := uuid.New()
	_, xerr = f.svc.Start(context.Background(), scope, actor, m.ID, models.StartToolMigration{
		Options: models.ToolMigrationOptions{Kinds: []source.Kind{source.KindLeads}},
		Targets: []models.ToolMigrationTargetChoice{{WorkspaceID: "a", CreateName: "New client"}, {WorkspaceID: "b", OrganizationID: &stranger}},
	})
	if xerr == nil || xerr.Identifier != CodeTargetInvalid {
		t.Fatalf("foreign workspace: %v", xerr)
	}
	if len(agencies.created) != 0 {
		t.Fatal("a client was created before every target was checked")
	}

	// Another agency's scope cannot see it at all.
	otherAgency := uuid.New()
	if _, xerr := f.svc.Get(context.Background(), Scope{AgencyID: &otherAgency}, m.ID); xerr == nil {
		t.Fatal("another agency read the migration")
	}

	agencies.role = models.AgencyRoleMember
	if _, xerr := f.svc.Start(context.Background(), scope, actor, m.ID, models.StartToolMigration{
		Options: models.ToolMigrationOptions{Kinds: []source.Kind{source.KindLeads}},
		Targets: []models.ToolMigrationTargetChoice{{WorkspaceID: "a", OrganizationID: &f.org}},
	}); xerr == nil || xerr.Identifier != CodePermission {
		t.Fatalf("non-admin staff: %v", xerr)
	}
}

func TestDismissRefusesOnlyWhileRunning(t *testing.T) {
	f := newFixture()
	m, xerr := f.svc.Create(context.Background(), f.scope(), f.actor(), models.NewToolMigration{Provider: "faketool", Credentials: []source.Credentials{{"api_key": "k"}}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	// Still scanning: going back to change the key deletes it.
	if xerr := f.svc.Dismiss(context.Background(), f.scope(), f.actor(), m.ID); xerr != nil {
		t.Fatalf("dismiss while scanning: %v", xerr)
	}
	if _, ok := f.repo.migs[m.ID]; ok {
		t.Fatal("dismissed migration still stored")
	}
}

func hasNote(notes []string, part string) bool {
	for _, n := range notes {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

// A workspace whose campaigns were all left out, on a tool that cannot list leads outside campaigns, still completes.
func TestRunWithNoCampaignLeadsAndNoAccountListingCompletes(t *testing.T) {
	f := newFixture()
	f.provider.campaigns = []source.Campaign{{ID: "done", Name: "Old", Status: source.CampaignCompleted,
		Steps: []source.Step{{Variants: []source.Variant{{Subject: "Hi", Body: "Hello"}}}}}}
	f.provider.leads = map[string][]source.Lead{"done": {{Email: "x@lead.com", Status: source.LeadCompleted}}}
	m := f.ready(t)
	f.start(t, m, models.ToolMigrationOptions{Kinds: source.AllKinds, SkipFinished: true, InFlight: models.ToolMigrationSkip})

	m, _ = f.svc.Get(context.Background(), f.scope(), m.ID)
	tg := m.Targets[0]
	if tg.Status != models.ToolMigrationTargetDone || m.Status != models.ToolMigrationDone {
		t.Fatalf("target %s (%s), migration %s; want both completed", tg.Status, tg.Error, m.Status)
	}
	if !hasNote(tg.Followup.Notes, "outside its campaigns") {
		t.Errorf("no note about leads outside campaigns in %v", tg.Followup.Notes)
	}
}
