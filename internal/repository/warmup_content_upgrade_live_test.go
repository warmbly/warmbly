package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Requires an explicitly configured, empty, disposable local database.
func TestLiveWarmupContentUpgradePreservesLegacyAndSerializesStops(t *testing.T) {
	dsn := os.Getenv("WARMBLY_CONTENT_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_CONTENT_TEST_DB not set")
	}
	path, err := filepath.Abs("../infrastructure/db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+path, dsn)
	if err != nil {
		t.Fatal("open local migration runner")
	}
	defer m.Close()
	if _, _, err := m.Version(); err != migrate.ErrNilVersion {
		t.Fatal("content upgrade fixture requires an empty database")
	}
	if err := m.Migrate(265); err != nil {
		t.Fatalf("released baseline migration: %v", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("connect local fixture")
	}
	defer pool.Close()
	legacyID, oldJobID := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO warmup_conversations (id,pool_type,source,subject,description,messages) VALUES ($1,'premium','ai','Legacy subject','Legacy opening','["Legacy continuation", "Legacy closure"]')`, legacyID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO warmup_generation_jobs (id,mode,trigger,pool_type,model,requested_count,status,batch_id,batch_status) VALUES ($1,'batch','manual','premium','legacy-visible-model',1,'running','existing-provider-job','in_progress')`, oldJobID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO admin_settings (key,value) VALUES ('warmup_generation','{"model":"legacy-visible-model","enabled":false,"cadence_hours":24,"refresh_per_run":0}')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("content additive upgrade: %v", err)
	}
	r := NewWarmupContentRepository(pool)
	c, err := r.GetConversation(ctx, legacyID)
	if err != nil || c == nil {
		t.Fatalf("legacy conversation unavailable: %v", err)
	}
	if c.Subject != "Legacy subject" || c.Messages[0] != "Legacy continuation" || c.SemanticReview != "legacy_unknown" || c.ScenarioVersion != nil || c.Status != "active" {
		t.Fatalf("legacy data changed: %+v", c)
	}
	j, err := r.GetGenerationJob(ctx, oldJobID)
	if err != nil || j == nil || j.BatchID != "existing-provider-job" || j.ContentVersion != nil || j.Model != "legacy-visible-model" {
		t.Fatalf("legacy job not preserved: %+v, %v", j, err)
	}
	settings, err := r.GetGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.GenerationEnabled || settings.Enabled || settings.Model != "legacy-visible-model" || settings.CadenceHours != 24 || settings.RefreshPerRun != 0 {
		t.Fatalf("legacy settings changed: %+v", settings)
	}
	settings.GenerationEnabled = false
	if err := r.PutGenerationSettings(ctx, *settings, nil); err != nil {
		t.Fatal(err)
	}
	newJob := func() *models.WarmupGenerationJob {
		version := generation.DiagnosticScenarioVersion
		return &models.WarmupGenerationJob{ID: uuid.New(), PoolType: "premium", Trigger: "manual", Model: settings.Model, RequestedCount: 1, Status: "pending", ContentVersion: &version}
	}
	if err := r.CreateGenerationJob(ctx, newJob()); err == nil {
		t.Fatal("committed stop allowed reservation")
	}
	jobs, err := r.ListActiveBatchJobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != oldJobID {
		t.Fatalf("stop lost outstanding batch: %+v, %v", jobs, err)
	}
	j.BatchStatus = "finalizing"
	if err := r.UpdateGenerationJob(ctx, j); err != nil {
		t.Fatal("stop blocked existing job update")
	}
	settings.GenerationEnabled, settings.DailyGenerationCap = true, 2
	if err := r.PutGenerationSettings(ctx, *settings, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- r.CreateGenerationJob(ctx, newJob()) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !strings.Contains(err.Error(), "reservation cap") {
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("atomic daily cap admitted %d competing requests, want 1", success)
	}
	reserved, err := r.ReservedGenerationCountSince(ctx, time.Now().UTC().Truncate(24*time.Hour))
	if err != nil || reserved != 2 {
		t.Fatalf("reservation cap accounting: %d, %v", reserved, err)
	}
	version, rendering := generation.DiagnosticScenarioVersion, generation.CanonicalRenderingVersion
	archived := &models.WarmupConversation{ID: uuid.New(), PoolType: "premium", Source: "ai", Subject: "Simulated diagnostic: unavailable review", Description: "Simulated diagnostic. A hypothetical example.", Messages: []string{"The example is closed."}, Status: "archived", SemanticReview: "unavailable", ScenarioVersion: &version, RenderingVersion: &rendering}
	if err := r.InsertConversation(ctx, archived); err != nil {
		t.Fatal(err)
	}
	if err := r.SetConversationStatus(ctx, archived.ID, "active"); err == nil {
		t.Fatal("unavailable judge treated as semantic approval")
	}
	if err := r.SetConversationStatus(ctx, legacyID, "archived"); err != nil {
		t.Fatal(err)
	}
	retired, err := r.GetConversation(ctx, legacyID)
	if err != nil || retired == nil || retired.Subject != "Legacy subject" {
		t.Fatal("retirement discarded historical source")
	}
	if got, err := r.GetConversation(ctx, uuid.New()); err != nil || got != nil {
		t.Fatal("missing source resolved to unrelated content")
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("additive downgrade: %v", err)
	}
	var oldSubject string
	if err := pool.QueryRow(ctx, `SELECT subject FROM warmup_conversations WHERE id=$1`, legacyID).Scan(&oldSubject); err != nil || oldSubject != "Legacy subject" {
		t.Fatal("rollback changed legacy content")
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("additive re-upgrade: %v", err)
	}
}
