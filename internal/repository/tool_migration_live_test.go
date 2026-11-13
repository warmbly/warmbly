package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// A migration from another tool is scoped to its workspace, worked under
// leases, and gives up its keys when it ends.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<scratch>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveToolMigration -v
func TestLiveToolMigrationLifecycle(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 396)
	ctx := context.Background()
	user, org, other := uuid.New(), uuid.New(), uuid.New()
	tag := org.String()[:8]
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Tool', 'Migration', $2, 'x')`, user, "toolmig-"+tag+"@fixture.invalid")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Mig', $2, $3), ($4, 'Other', $5, $3)`,
		org, "toolmig-"+tag+"-a", user, other, "toolmig-"+tag+"-b")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{org, other})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})

	repo := NewToolMigrationRepository(handle)
	scope, wrong := ToolMigrationScope{OrgID: &org}, ToolMigrationScope{OrgID: &other}
	m := &models.ToolMigration{ID: uuid.New(), OrganizationID: &org, CreatedByUserID: &user, Provider: "smartlead", Label: "Smartlead", Status: models.ToolMigrationScanning}
	if err := repo.Create(ctx, m, "sealed-value", true); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, wrong, m.ID); err != nil || got != nil {
		t.Fatalf("another workspace read it: %v %v", got, err)
	}
	if _, err := repo.Get(ctx, ToolMigrationScope{}, m.ID); err == nil {
		t.Fatal("an empty scope was accepted")
	}

	scan, err := repo.ClaimScan(ctx, time.Minute)
	if err != nil || scan == nil || scan.ID != m.ID || scan.Sealed != "sealed-value" {
		t.Fatalf("claim scan: %+v %v", scan, err)
	}
	if again, _ := repo.ClaimScan(ctx, time.Minute); again != nil && again.ID == m.ID {
		t.Fatal("a leased scan was claimed twice")
	}
	if ok, _ := repo.FinishScan(ctx, m.ID, uuid.New(), nil, models.ToolMigrationFailed, "stale"); ok {
		t.Fatal("a holder without the lease token finished the scan")
	}
	if ok, err := repo.RenewScan(ctx, m.ID, scan.Token, time.Minute); err != nil || !ok {
		t.Fatalf("renew scan: %v %v", ok, err)
	}
	if ok, err := repo.FinishScan(ctx, m.ID, scan.Token, []byte(`[{"workspace":{"id":"","name":"Main"},"inventory":{"leads":-1}}]`), models.ToolMigrationReady, ""); err != nil || !ok {
		t.Fatalf("finish scan: %v %v", ok, err)
	}

	if ok, _, err := repo.ClaimStart(ctx, wrong, m.ID, time.Minute); err != nil || ok {
		t.Fatalf("another workspace claimed the start: %v %v", ok, err)
	}
	if ok, err := repo.Start(ctx, scope, m.ID, []byte(`{}`), nil); err != nil || ok {
		t.Fatalf("started without holding the start: %v %v", ok, err)
	}
	claimed, created, err := repo.ClaimStart(ctx, scope, m.ID, time.Minute)
	if err != nil || !claimed || len(created) != 0 {
		t.Fatalf("claim start: %v %v %v", claimed, created, err)
	}
	if again, _, _ := repo.ClaimStart(ctx, scope, m.ID, time.Minute); again {
		t.Fatal("a held start was claimed twice")
	}
	client := uuid.New()
	if err := repo.RecordClient(ctx, m.ID, "0\x1fa", client); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReleaseStart(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, created, _ = repo.ClaimStart(ctx, scope, m.ID, time.Minute); created["0\x1fa"] != client {
		t.Fatalf("a retried start lost the client it made: %v", created)
	}
	target := uuid.New()
	if ok, err := repo.Start(ctx, scope, m.ID, []byte(`{"kinds":["leads"],"in_flight":"skip"}`),
		[]ToolMigrationTargetInsert{{ID: target, OrganizationID: org, WorkspaceName: "Main"}}); err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	if ok, _ := repo.Delete(ctx, scope, m.ID); ok {
		t.Fatal("a running migration was deleted")
	}

	job, err := repo.ClaimTarget(ctx, time.Minute)
	if err != nil || job == nil || job.ID != target || job.Sealed == nil || !job.ViaSession {
		t.Fatalf("claim target: %+v %v", job, err)
	}
	stale := uuid.New()
	contact := uuid.New()
	if held, err := repo.PutItems(ctx, target, stale, org, "contact", map[string]uuid.UUID{"b@x.com": contact}); err != nil || held {
		t.Fatalf("a stale holder recorded items: %v %v", held, err)
	}
	if held, err := repo.PutItems(ctx, target, job.Token, org, "contact", map[string]uuid.UUID{"a@x.com": contact}); err != nil || !held {
		t.Fatalf("put items: %v %v", held, err)
	}
	if _, err := repo.PutItems(ctx, target, job.Token, org, "contact", map[string]uuid.UUID{"a@x.com": uuid.New()}); err != nil {
		t.Fatal(err)
	}
	if state, _, err := repo.ReserveItem(ctx, target, stale, org, "campaign", "c1"); err != nil || state != ItemLost {
		t.Fatalf("a stale holder reserved: %v %v", state, err)
	}
	if state, _, err := repo.ReserveItem(ctx, target, job.Token, org, "campaign", "c1"); err != nil || state != ItemReserved {
		t.Fatalf("reserve: %v %v", state, err)
	}
	if state, _, _ := repo.ReserveItem(ctx, target, job.Token, org, "campaign", "c1"); state != ItemPending {
		t.Fatalf("an unfinished reservation reads %v; want pending", state)
	}
	if _, ok, _ := repo.Item(ctx, target, "campaign", "c1"); ok {
		t.Fatal("a pending reservation read as done")
	}
	camp := uuid.New()
	if held, err := repo.CompleteItem(ctx, target, job.Token, "campaign", "c1", camp); err != nil || !held {
		t.Fatalf("complete: %v %v", held, err)
	}
	if state, id, _ := repo.ReserveItem(ctx, target, job.Token, org, "campaign", "c1"); state != ItemDone || id != camp {
		t.Fatalf("a completed item reads %v %v", state, id)
	}
	if ok, _ := repo.RenewTarget(ctx, target, stale, time.Minute); ok {
		t.Fatal("a stale holder renewed the lease")
	}
	if seen, _ := repo.ItemsIn(ctx, target, "contact", []string{"a@x.com", "b@x.com"}); !seen["a@x.com"] || seen["b@x.com"] {
		t.Fatalf("items: %v", seen)
	}
	if id, ok, _ := repo.Item(ctx, target, "contact", "a@x.com"); !ok || id != contact {
		t.Fatal("a second write replaced the first item")
	}
	if ok, err := repo.SaveTarget(ctx, target, stale, time.Minute, ToolMigrationProgress{Phase: "leads"}); err != nil || ok {
		t.Fatal("a stale holder checkpointed")
	}
	if ok, err := repo.SaveTarget(ctx, target, job.Token, time.Minute, ToolMigrationProgress{Phase: "leads", Cursor: []byte(`{"page":"2"}`), Counts: []byte(`{"leads_read":5}`)}); err != nil || !ok {
		t.Fatalf("save: %v %v", ok, err)
	}
	if status, _ := repo.Finalize(ctx, m.ID); status != "" {
		t.Fatal("finalized with a target still running")
	}
	if ok, err := repo.FinishTarget(ctx, target, job.Token, models.ToolMigrationTargetDone, ToolMigrationProgress{Phase: "leads"}, ""); err != nil || !ok {
		t.Fatalf("finish target: %v %v", ok, err)
	}
	if status, err := repo.Finalize(ctx, m.ID); err != nil || status != models.ToolMigrationDone {
		t.Fatalf("finalize: %q %v", status, err)
	}
	got, err := repo.Get(ctx, scope, m.ID)
	if err != nil || got.CredentialsHeld || got.Status != models.ToolMigrationDone || len(got.Targets) != 1 || got.Targets[0].Counts.LeadsRead != 5 || got.Options == nil {
		t.Fatalf("after finish: %+v %v", got, err)
	}
	if list, err := repo.List(ctx, scope, nil, nil, 10); err != nil || len(list) != 1 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if n, _ := repo.CountActive(ctx, scope); n != 0 {
		t.Fatalf("active = %d", n)
	}
	if _, err := repo.Upkeep(ctx, time.Hour, 5); err != nil {
		t.Fatalf("upkeep: %v", err)
	}
	if ok, err := repo.Delete(ctx, scope, m.ID); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
}

// A write racing the target's removal reads as a lost lease, never an error.
func TestLiveToolMigrationWritesAfterRemovalStopQuietly(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 396)
	ctx := context.Background()
	user, org := uuid.New(), uuid.New()
	tag := org.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Tool', 'Gone', $2, 'x')`, user, "toolgone-"+tag+"@fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Gone', $2, $3)`, org, "toolgone-"+tag, user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})
	repo := NewToolMigrationRepository(handle)
	scope := ToolMigrationScope{OrgID: &org}
	m := &models.ToolMigration{ID: uuid.New(), OrganizationID: &org, CreatedByUserID: &user, Provider: "smartlead", Status: models.ToolMigrationScanning}
	if err := repo.Create(ctx, m, "sealed", false); err != nil {
		t.Fatal(err)
	}
	scan, _ := repo.ClaimScan(ctx, time.Minute)
	if scan == nil {
		t.Fatal("no scan to claim")
	}
	_, _ = repo.FinishScan(ctx, m.ID, scan.Token, []byte(`[]`), models.ToolMigrationReady, "")
	_, _, _ = repo.ClaimStart(ctx, scope, m.ID, time.Minute)
	if ok, err := repo.Start(ctx, scope, m.ID, []byte(`{}`), []ToolMigrationTargetInsert{{ID: uuid.New(), OrganizationID: org}}); err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	job, _ := repo.ClaimTarget(ctx, time.Minute)
	if job == nil || job.MigrationID != m.ID {
		t.Fatalf("claim: %+v", job)
	}
	if ok, err := repo.Cancel(ctx, scope, m.ID); err != nil || !ok {
		t.Fatalf("cancel: %v %v", ok, err)
	}
	if ok, err := repo.Delete(ctx, scope, m.ID); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if held, err := repo.PutItems(ctx, job.ID, job.Token, org, "contact", map[string]uuid.UUID{"a@x.com": uuid.New()}); err != nil || held {
		t.Fatalf("put items after removal: %v %v", held, err)
	}
	if state, _, err := repo.ReserveItem(ctx, job.ID, job.Token, org, "campaign", "c"); err != nil || state != ItemLost {
		t.Fatalf("reserve after removal: %v %v", state, err)
	}
	if ok, err := repo.SaveTarget(ctx, job.ID, job.Token, time.Minute, ToolMigrationProgress{}); err != nil || ok {
		t.Fatalf("save after removal: %v %v", ok, err)
	}
}
