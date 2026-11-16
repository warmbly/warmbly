package toolmigrate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

func TestActorForNarrowsKeysToTheirScopes(t *testing.T) {
	user := uuid.New()
	readOnly := models.APIPermImportTools | models.APIPermReadCampaigns | models.APIPermReadEmails
	a := ActorFor(user, models.AllPermissions, readOnly, true, true)
	if a.OrgPerms.HasPermission(models.PermManageCampaigns) || a.OrgPerms.HasPermission(models.PermManageEmails) || a.ViaSession {
		t.Fatalf("a read-scoped key kept write permissions or a session: %+v", a)
	}
	if !a.OrgPerms.HasPermission(models.PermManageContacts) {
		t.Fatal("the contacts permission was dropped")
	}
	full := ActorFor(user, models.AllPermissions, readOnly|models.APIPermWriteCampaigns|models.APIPermWriteEmails, true, false)
	if !full.OrgPerms.HasPermission(models.PermManageCampaigns) || !full.OrgPerms.HasPermission(models.PermManageEmails) {
		t.Fatal("a key with the write scopes lost them")
	}
	if s := ActorFor(user, models.AllPermissions, 0, false, true); !s.ViaSession || !s.OrgPerms.HasPermission(models.PermManageCampaigns) {
		t.Fatalf("a dashboard session was narrowed: %+v", s)
	}
	if s := ActorFor(user, models.AllPermissions, 0, false, false); s.ViaSession {
		t.Fatal("a member acting outside the dashboard counted as a session")
	}
}

func TestAgencyKeyCheck(t *testing.T) {
	migrate := models.APIPermAgencyMigrate
	for name, c := range map[string]struct {
		perms          uint64
		kinds          []source.Kind
		creates, links bool
		ok             bool
	}{
		"leads only":                {migrate, []source.Kind{source.KindLeads}, false, false, true},
		"campaigns without write":   {migrate, []source.Kind{source.KindCampaigns}, false, false, false},
		"campaigns with write":      {migrate | models.APIPermWriteCampaigns, []source.Kind{source.KindCampaigns}, false, false, true},
		"mailboxes without write":   {migrate | models.APIPermWriteCampaigns, []source.Kind{source.KindMailboxes}, false, false, false},
		"new client without bit":    {migrate, []source.Kind{source.KindLeads}, true, false, false},
		"link senders without bit":  {migrate, nil, false, true, false},
		"link senders with the bit": {migrate | models.APIPermWriteCampaigns, nil, false, true, true},
	} {
		if xerr := AgencyKeyCheck(c.perms, c.kinds, c.creates, c.links); (xerr == nil) != c.ok {
			t.Errorf("%s: %v", name, xerr)
		}
	}
}

// steal hands every target to another holder, as a reclaim on another replica would.
func (f *fixture) steal() {
	f.repo.mu.Lock()
	defer f.repo.mu.Unlock()
	for _, tg := range f.repo.targets {
		tg.token = uuid.New()
	}
}

func TestALostLeaseStopsEveryWrite(t *testing.T) {
	f := newFixture()
	richSource(f.provider)
	m := f.ready(t)
	f.provider.onCampaigns = f.steal
	f.start(t, m, models.ToolMigrationOptions{Kinds: []source.Kind{source.KindCampaigns, source.KindLeads}})

	if len(f.campaigns.created) != 0 || len(f.contacts.calls) != 0 {
		t.Fatalf("a holder that lost its lease wrote: campaigns=%d imports=%d", len(f.campaigns.created), len(f.contacts.calls))
	}
	for _, tg := range f.repo.targets {
		if tg.t.Status != models.ToolMigrationTargetRunning || len(f.repo.items) != 0 {
			t.Fatalf("the stale holder finished or recorded: %s, items %d", tg.t.Status, len(f.repo.items))
		}
	}
}

func TestTheHeartbeatStopsProviderWorkOnceTheLeaseIsGone(t *testing.T) {
	f := newFixture()
	richSource(f.provider)
	f.svc.renewEvery = 5 * time.Millisecond
	m := f.ready(t)
	stopped := false
	f.provider.onLeads = func(ctx context.Context) error {
		f.steal()
		select {
		case <-ctx.Done():
			stopped = true
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	}
	f.start(t, m, models.ToolMigrationOptions{Kinds: []source.Kind{source.KindLeads}})
	if !stopped {
		t.Fatal("provider work kept going after the lease was lost")
	}
	if len(f.contacts.calls) != 0 {
		t.Fatal("leads were imported after the lease was lost")
	}
}

func TestDismissMidPageStopsQuietly(t *testing.T) {
	f := newFixture()
	richSource(f.provider)
	m := f.ready(t)
	f.provider.onLeads = func(context.Context) error {
		f.provider.onLeads = nil
		if _, xerr := f.svc.Cancel(context.Background(), f.scope(), f.actor(), m.ID); xerr != nil {
			t.Errorf("cancel: %v", xerr)
		}
		if xerr := f.svc.Dismiss(context.Background(), f.scope(), f.actor(), m.ID); xerr != nil {
			t.Errorf("dismiss: %v", xerr)
		}
		return nil
	}
	f.start(t, m, models.ToolMigrationOptions{Kinds: []source.Kind{source.KindLeads}})
	if len(f.repo.targets) != 0 || len(f.repo.items) != 0 {
		t.Fatalf("a removed migration was written to: targets=%d items=%d", len(f.repo.targets), len(f.repo.items))
	}
}

// agencyFixture is a ready agency migration over two source workspaces.
func agencyFixture(t *testing.T) (*fixture, *fakeAgencies, Scope, Actor, uuid.UUID) {
	t.Helper()
	f := newFixture()
	f.provider.workspace = []source.Workspace{{ID: "a", Name: "Client A"}, {ID: "b", Name: "Client B"}}
	agencyID := uuid.New()
	agencies := &fakeAgencies{role: models.AgencyRoleOwner, members: f.members}
	f.svc.d.Agencies = agencies
	f.svc.d.AgencyClients = fakeAgencyClients{{OrganizationID: f.org}}
	scope := Scope{AgencyID: &agencyID}
	actor := Actor{UserID: f.user}
	m, xerr := f.svc.Create(context.Background(), scope, actor, models.NewToolMigration{Provider: "faketool", Credentials: []source.Credentials{{"api_key": "k"}}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	f.svc.pass(context.Background())
	return f, agencies, scope, actor, m.ID
}

func TestAgencyStartRetryReusesTheClientsItCreated(t *testing.T) {
	f, agencies, scope, actor, id := agencyFixture(t)
	ctx := context.Background()
	start := func(second string) (*models.ToolMigration, *errx.Error) {
		return f.svc.Start(ctx, scope, actor, id, models.StartToolMigration{
			Options: models.ToolMigrationOptions{Kinds: []source.Kind{source.KindLeads}},
			Targets: []models.ToolMigrationTargetChoice{{WorkspaceID: "a", CreateName: "Client A"}, {WorkspaceID: "b", CreateName: second}},
		})
	}
	if _, xerr := start("fail"); xerr == nil {
		t.Fatal("a failed client creation started the migration")
	}
	m, xerr := start("Client B")
	if xerr != nil {
		t.Fatalf("retry: %v", xerr)
	}
	if want := []string{"Client A", "fail", "Client B"}; !equal(agencies.created, want) {
		t.Fatalf("clients created %v; want %v (the retry reuses Client A)", agencies.created, want)
	}
	if m.Status != models.ToolMigrationRunning || len(m.Targets) != 2 || m.Targets[0].OrganizationID != f.repo.migs[id].created[choiceKey("a", 0)] {
		t.Fatalf("started %+v", m)
	}
	// A third call is a retry of a started migration: nothing more is created.
	if _, xerr := start("Client B"); xerr != nil || len(agencies.created) != 3 {
		t.Fatalf("a repeated start created more: %v %v", xerr, agencies.created)
	}
}

func TestAgencyStartCreatesNothingWhileAnotherStartHoldsIt(t *testing.T) {
	f, agencies, scope, actor, id := agencyFixture(t)
	f.repo.onClaimStart = func() {
		f.repo.mu.Lock()
		f.repo.migs[id].starting = true
		f.repo.mu.Unlock()
	}
	_, xerr := f.svc.Start(context.Background(), scope, actor, id, models.StartToolMigration{
		Options: models.ToolMigrationOptions{Kinds: []source.Kind{source.KindLeads}},
		Targets: []models.ToolMigrationTargetChoice{{WorkspaceID: "a", CreateName: "Client A"}, {WorkspaceID: "b", Skip: true}},
	})
	if xerr == nil || xerr.Code != errx.Conflict {
		t.Fatalf("got %v; want a conflict", xerr)
	}
	if len(agencies.created) != 0 {
		t.Fatalf("a client was created by a start that did not hold the migration: %v", agencies.created)
	}
}

func TestAgencyTargetFailsWhenTheCreatorIsNoLongerAnAdmin(t *testing.T) {
	f, agencies, scope, actor, id := agencyFixture(t)
	if _, xerr := f.svc.Start(context.Background(), scope, actor, id, models.StartToolMigration{
		Options: models.ToolMigrationOptions{Kinds: []source.Kind{source.KindLeads}},
		Targets: []models.ToolMigrationTargetChoice{{WorkspaceID: "a", OrganizationID: &f.org}, {WorkspaceID: "b", Skip: true}},
	}); xerr != nil {
		t.Fatal(xerr)
	}
	agencies.role = models.AgencyRoleMember
	f.svc.pass(context.Background())
	m, _ := f.svc.Get(context.Background(), scope, id)
	if m.Targets[0].Status != models.ToolMigrationTargetFailed || len(f.contacts.calls) != 0 {
		t.Fatalf("target %s, imports %d", m.Targets[0].Status, len(f.contacts.calls))
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
