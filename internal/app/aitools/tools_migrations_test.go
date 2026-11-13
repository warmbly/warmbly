package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/toolmigrate"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// fakeMigrations records the scope and actor of the last call.
type fakeMigrations struct {
	scope   toolmigrate.Scope
	actor   toolmigrate.Actor
	created models.NewToolMigration
	started models.StartToolMigration
	calls   int
}

func (f *fakeMigrations) Providers() []source.Info {
	return []source.Info{{ID: "smartlead", Label: "Smartlead"}}
}

func (f *fakeMigrations) Create(_ context.Context, scope toolmigrate.Scope, actor toolmigrate.Actor, in models.NewToolMigration) (*models.ToolMigration, *errx.Error) {
	f.calls++
	f.scope, f.actor, f.created = scope, actor, in
	return &models.ToolMigration{ID: uuid.New(), Provider: in.Provider, Status: models.ToolMigrationScanning, CredentialsHeld: true}, nil
}

func (f *fakeMigrations) List(_ context.Context, scope toolmigrate.Scope, _ string, _ int) (*models.ToolMigrationList, *errx.Error) {
	f.calls++
	f.scope = scope
	return &models.ToolMigrationList{}, nil
}

func (f *fakeMigrations) Get(_ context.Context, scope toolmigrate.Scope, id uuid.UUID) (*models.ToolMigration, *errx.Error) {
	f.calls++
	f.scope = scope
	return &models.ToolMigration{ID: id}, nil
}

func (f *fakeMigrations) Start(_ context.Context, scope toolmigrate.Scope, actor toolmigrate.Actor, id uuid.UUID, in models.StartToolMigration) (*models.ToolMigration, *errx.Error) {
	f.calls++
	f.scope, f.actor, f.started = scope, actor, in
	return &models.ToolMigration{ID: id, Status: models.ToolMigrationRunning}, nil
}

func (f *fakeMigrations) Cancel(_ context.Context, scope toolmigrate.Scope, actor toolmigrate.Actor, id uuid.UUID) (*models.ToolMigration, *errx.Error) {
	f.calls++
	f.scope, f.actor = scope, actor
	return &models.ToolMigration{ID: id}, nil
}

func (f *fakeMigrations) LinkSenders(_ context.Context, scope toolmigrate.Scope, actor toolmigrate.Actor, id uuid.UUID) (*models.ToolMigration, *errx.Error) {
	f.calls++
	f.scope, f.actor = scope, actor
	return &models.ToolMigration{ID: id}, nil
}

func (f *fakeMigrations) Dismiss(context.Context, toolmigrate.Scope, toolmigrate.Actor, uuid.UUID) *errx.Error {
	f.calls++
	return nil
}

var migrationToolNames = []string{"list_migration_providers", "create_migration", "list_migrations", "get_migration", "start_migration", "cancel_migration", "link_migration_senders"}

func TestMigrationToolsAreGatedLikeTheirRoutes(t *testing.T) {
	r := BuildRegistry(Deps{ToolMigrations: &fakeMigrations{}})
	for _, name := range migrationToolNames {
		tool, ok := r.Get(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if tool.RequiredAPIPerm != models.APIPermImportTools || tool.RequiredOrgPerm != models.PermManageContacts {
			t.Errorf("%s gates = %b / %b", name, tool.RequiredAPIPerm, tool.RequiredOrgPerm)
		}
		for who, inv := range map[string]Invocation{
			"key without IMPORT_TOOLS":         {IsAPIKey: true, APIPerms: models.AllAPIPermissionsMask &^ models.APIPermImportTools},
			"token whose member cannot import": {IsAPIKey: true, ActsForMember: true, APIPerms: models.APIPermImportTools, OrgPerms: models.PermViewContacts},
			"session without manage contacts":  {OrgPerms: models.PermViewContacts | models.PermManageCampaigns},
		} {
			if r.Permits(inv, name) {
				t.Errorf("%s permitted to %s", name, who)
			}
		}
		if !r.Permits(Invocation{IsAPIKey: true, ActsForMember: true, APIPerms: models.APIPermImportTools, OrgPerms: models.PermManageContacts}, name) {
			t.Errorf("%s refused to a key with IMPORT_TOOLS acting for a member who manages contacts", name)
		}
	}
	if start, _ := r.Get("start_migration"); !start.AlwaysAsk || !strings.Contains(start.Description, "never launched") {
		t.Error("start_migration must always ask and say imported campaigns are drafts")
	}
	if r := BuildRegistry(Deps{}); r.Permits(Invocation{OrgPerms: models.AllPermissions}, "create_migration") {
		t.Error("migration tools registered without the service")
	}
}

func TestMigrationToolsRunInTheCallersWorkspace(t *testing.T) {
	fake := &fakeMigrations{}
	r := BuildRegistry(Deps{ToolMigrations: fake})
	org, user := uuid.New(), uuid.New()
	key := Invocation{OrgID: org, UserID: user, IsAPIKey: true, ActsForMember: true, APIPerms: models.APIPermImportTools, OrgPerms: models.PermManageContacts}

	out, err := r.Call(context.Background(), key, "create_migration", json.RawMessage(`{"provider":"smartlead","credentials":[{"api_key":"sl-secret-value"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if fake.scope.OrgID == nil || *fake.scope.OrgID != org || fake.scope.AgencyID != nil {
		t.Errorf("scope = %+v, want the caller's workspace only", fake.scope)
	}
	if fake.actor.UserID != user || fake.actor.ViaSession || fake.actor.OrgPerms != models.PermManageContacts {
		t.Errorf("actor = %+v; a key is never a session", fake.actor)
	}
	if fake.created.Credentials[0]["api_key"] != "sl-secret-value" {
		t.Errorf("credentials did not reach the service: %+v", fake.created)
	}
	if strings.Contains(out, "sl-secret-value") {
		t.Errorf("the result echoed a credential: %s", out)
	}

	session := Invocation{OrgID: org, UserID: user, OrgPerms: models.PermManageContacts, ViaDashboardSession: true}
	if _, err := r.Call(context.Background(), session, "start_migration", json.RawMessage(`{"migration_id":"`+uuid.NewString()+`"}`)); err != nil {
		t.Fatal(err)
	}
	if !fake.actor.ViaSession {
		t.Error("a dashboard session was not passed as one")
	}
	if len(fake.started.Options.Kinds) != len(source.AllKinds) || fake.started.Options.InFlight != models.ToolMigrationSkip {
		t.Errorf("defaults = %+v", fake.started.Options)
	}

	// A workspace migration lands in the caller's own workspace; it names no other.
	calls := fake.calls
	_, err = r.Call(context.Background(), key, "start_migration", json.RawMessage(`{"migration_id":"`+uuid.NewString()+`","targets":[{"workspace_id":"1","organization_id":"`+uuid.NewString()+`"}]}`))
	if !errors.Is(err, ErrInvalidArgs) || fake.calls != calls {
		t.Errorf("workspace start with targets = %v, service calls %d -> %d", err, calls, fake.calls)
	}
}

func TestAgencyToolsNeedAgencyBits(t *testing.T) {
	fake := &fakeMigrations{}
	r := BuildAgencyRegistry(AgencyDeps{ToolMigrations: fake})
	agency, user := uuid.New(), uuid.New()
	ctx := context.Background()

	for _, name := range migrationToolNames {
		if _, err := r.Call(ctx, AgencyInvocation{AgencyID: agency, UserID: user, APIPerms: models.APIPermImportTools | models.APIPermAgencyRead}, name, json.RawMessage(`{}`)); !errors.Is(err, ErrToolForbidden) {
			t.Errorf("%s without AGENCY_MIGRATE = %v", name, err)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("service ran %d times for refused calls", fake.calls)
	}

	migrate := AgencyInvocation{AgencyID: agency, UserID: user, APIPerms: models.APIPermAgencyMigrate}
	if _, err := r.Call(ctx, migrate, "get_migration", json.RawMessage(`{"migration_id":"`+uuid.NewString()+`"}`)); err != nil {
		t.Fatal(err)
	}
	if fake.scope.AgencyID == nil || *fake.scope.AgencyID != agency || fake.scope.OrgID != nil {
		t.Errorf("scope = %+v, want the agency only", fake.scope)
	}

	// Creating a client through a migration needs the bit that creates clients.
	create := `{"migration_id":"` + uuid.NewString() + `","targets":[{"workspace_id":"1","create_name":"Acme"}]}`
	calls := fake.calls
	if _, err := r.Call(ctx, migrate, "start_migration", json.RawMessage(create)); err == nil || fake.calls != calls {
		t.Errorf("start creating a client without AGENCY_MANAGE_CLIENTS = %v, service calls %d -> %d", err, calls, fake.calls)
	}
	migrate.APIPerms |= models.APIPermAgencyManageClients | models.APIPermWriteCampaigns | models.APIPermWriteEmails
	if _, err := r.Call(ctx, migrate, "start_migration", json.RawMessage(create)); err != nil {
		t.Fatal(err)
	}
	if len(fake.started.Targets) != 1 || fake.started.Targets[0].CreateName != "Acme" || fake.actor.ViaSession {
		t.Errorf("agency start = %+v actor %+v", fake.started, fake.actor)
	}
	existing := `{"migration_id":"` + uuid.NewString() + `","kinds":["leads"],"targets":[{"workspace_id":"1","organization_id":"` + uuid.NewString() + `"}]}`
	if _, err := r.Call(ctx, AgencyInvocation{AgencyID: agency, UserID: user, APIPerms: models.APIPermAgencyMigrate}, "start_migration", json.RawMessage(existing)); err != nil {
		t.Errorf("mapping onto an existing client needs only AGENCY_MIGRATE: %v", err)
	}

	for _, tool := range BuildAgencyRegistry(AgencyDeps{Agency: &stubAgency{}, Home: &stubHome{}, ToolMigrations: fake}).PermittedTools(AgencyInvocation{APIPerms: models.AgencyAPIPermissionsMask | models.AllAPIPermissionsMask}) {
		if tool.RequiredAPIPerm&models.AgencyAPIPermissionsMask == 0 {
			t.Errorf("agency tool %s is gated by no agency bit", tool.Name)
		}
	}
}

type stubAgency struct{ AgencyClients }
type stubHome struct{ AgencyHome }

func TestMigrationToolsNarrowAKeyToItsScopes(t *testing.T) {
	fake := &fakeMigrations{}
	r := BuildRegistry(Deps{ToolMigrations: fake})
	key := Invocation{OrgID: uuid.New(), UserID: uuid.New(), IsAPIKey: true, ActsForMember: true,
		APIPerms: models.APIPermImportTools | models.APIPermReadCampaigns, OrgPerms: models.AllPermissions}
	for _, name := range []string{"start_migration", "link_migration_senders"} {
		if _, err := r.Call(context.Background(), key, name, json.RawMessage(`{"migration_id":"`+uuid.NewString()+`"}`)); err != nil {
			t.Fatal(err)
		}
		if fake.actor.OrgPerms.HasPermission(models.PermManageCampaigns) || fake.actor.OrgPerms.HasPermission(models.PermManageEmails) {
			t.Errorf("%s: a key without WRITE_CAMPAIGNS or WRITE_EMAILS acted with them: %b", name, fake.actor.OrgPerms)
		}
	}
	// The Slack agent and autopilot act as a member, never as a dashboard session.
	slack := Invocation{OrgID: uuid.New(), UserID: uuid.New(), OrgPerms: models.AllPermissions}
	if _, err := r.Call(context.Background(), slack, "start_migration", json.RawMessage(`{"migration_id":"`+uuid.NewString()+`"}`)); err != nil {
		t.Fatal(err)
	}
	if fake.actor.ViaSession {
		t.Error("a member acting outside the dashboard was treated as a signed-in session")
	}
}

func TestCreateMigrationIsOfferedOnlyToKeys(t *testing.T) {
	r := BuildRegistry(Deps{ToolMigrations: &fakeMigrations{}})
	for who, inv := range map[string]Invocation{
		"dashboard agent": {OrgPerms: models.AllPermissions, ViaDashboardSession: true},
		"Slack agent":     {OrgPerms: models.AllPermissions},
	} {
		if r.Permits(inv, "create_migration") {
			t.Errorf("create_migration offered to the %s, which keeps its arguments", who)
		}
		for _, def := range r.ToolDefs(context.Background(), inv) {
			if def.Name == "create_migration" {
				t.Errorf("create_migration in the %s's tool list", who)
			}
		}
		if _, err := r.Call(context.Background(), inv, "create_migration", json.RawMessage(`{"provider":"smartlead","credentials":[{"api_key":"x"}]}`)); !errors.Is(err, ErrToolForbidden) {
			t.Errorf("%s calling create_migration = %v", who, err)
		}
		if !r.Permits(inv, "list_migration_providers") || !r.Permits(inv, "start_migration") {
			t.Errorf("the %s lost the migration tools that carry no key", who)
		}
	}
	if providers, _ := r.Get("list_migration_providers"); !strings.Contains(providers.Description, "Settings > Migrate from another tool") {
		t.Error("the dashboard is not pointed at the migration page")
	}
	key := Invocation{IsAPIKey: true, ActsForMember: true, APIPerms: models.APIPermImportTools, OrgPerms: models.PermManageContacts}
	if !r.Permits(key, "create_migration") {
		t.Error("create_migration refused to a key")
	}
	agency := BuildAgencyRegistry(AgencyDeps{ToolMigrations: &fakeMigrations{}})
	if !agency.Has("create_migration") || len(agency.PermittedTools(AgencyInvocation{APIPerms: models.APIPermAgencyMigrate})) == 0 {
		t.Error("the agency registry, which only agency keys reach, lost create_migration")
	}
}

func TestAgencyMigrationToolsHoldTheKeyToItsWriteScopes(t *testing.T) {
	fake := &fakeMigrations{}
	r := BuildAgencyRegistry(AgencyDeps{ToolMigrations: fake})
	ctx := context.Background()
	inv := AgencyInvocation{AgencyID: uuid.New(), UserID: uuid.New(), APIPerms: models.APIPermAgencyMigrate}
	id := uuid.NewString()
	for name, args := range map[string]string{
		"start with campaigns":   `{"migration_id":"` + id + `","kinds":["campaigns"],"targets":[{"workspace_id":"1","skip":true}]}`,
		"start with mailboxes":   `{"migration_id":"` + id + `","kinds":["mailboxes"],"targets":[{"workspace_id":"1","skip":true}]}`,
		"start with every kind":  `{"migration_id":"` + id + `","targets":[{"workspace_id":"1","skip":true}]}`,
		"link_migration_senders": `{"migration_id":"` + id + `"}`,
	} {
		tool := "start_migration"
		if name == "link_migration_senders" {
			tool = name
		}
		calls := fake.calls
		if _, err := r.Call(ctx, inv, tool, json.RawMessage(args)); err == nil || fake.calls != calls {
			t.Errorf("%s without the write scope = %v, service calls %d -> %d", name, err, calls, fake.calls)
		}
	}
	inv.APIPerms |= models.APIPermWriteCampaigns
	if _, err := r.Call(ctx, inv, "link_migration_senders", json.RawMessage(`{"migration_id":"`+id+`"}`)); err != nil {
		t.Errorf("link with WRITE_CAMPAIGNS: %v", err)
	}
}
