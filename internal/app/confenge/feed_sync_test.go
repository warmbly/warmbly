package confenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestSyncFeedManifestIdempotentAndHashFailClosed(t *testing.T) {
	dir := t.TempDir()
	org := uuid.New()
	user := uuid.New()
	r := newMemRepo()
	svc := NewService(Config{
		Enabled: true, DefaultDailyLimit: 100, MaxInitialEmailWords: 120,
		RequireHumanApproval: true, MaxFeedPayloadBytes: 8 << 20,
	}, r, nil).(*service)

	lead := sampleLeadWithActivation(70, ActivationActionableNow)
	chunk := Feed{
		SchemaVersion: "confenge.outreach.v1",
		GeneratedAt:   "2026-08-08T10:00:00Z",
		Source: FeedSource{
			System: "extra-cli", RunID: "run-sync-1", SnapshotHash: "snap-sync-1",
			ProfileID: "p", ProfileVersion: "1",
		},
		Pagination: FeedPagination{HasMore: false},
		Leads:      []FeedLead{lead},
	}
	// Match schema constant
	chunk.SchemaVersion = modelsOutreachSchema()
	chunkRaw, err := json.MarshalIndent(chunk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	chunkRaw = append(chunkRaw, '\n')
	sum := sha256.Sum256(chunkRaw)
	chash := hex.EncodeToString(sum[:])
	chunkPath := filepath.Join(dir, "chunk_0000.json")
	if err := os.WriteFile(chunkPath, chunkRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	man := map[string]any{
		"schema_version": "confenge.outreach.manifest.v1",
		"generated_at":   "2026-08-08T10:00:00Z",
		"source": map[string]any{
			"system": "extra-cli", "run_id": "run-sync-1", "snapshot_hash": "snap-sync-1",
			"profile_id": "p", "profile_version": "1",
		},
		"lead_count":  1,
		"chunk_count": 1,
		"chunks": []map[string]any{
			{"file": "chunk_0000.json", "chunk_index": 0, "content_hash": chash, "lead_count": 1},
		},
		"deactivations": []any{},
	}
	manRaw, _ := json.MarshalIndent(man, "", "  ")
	manPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manPath, manRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + manPath

	ctx := context.Background()
	res, xerr := svc.SyncFeedManifest(ctx, org, &user, uri)
	if xerr != nil {
		t.Fatalf("sync1: %v", xerr)
	}
	if res.Status != "completed" || res.ChunksImported != 1 {
		t.Fatalf("unexpected res: %+v", res)
	}
	acc, _ := r.GetAccountByCNPJ(ctx, org, "11222333000181")
	if acc == nil || acc.ActivationState != ActivationActionableNow {
		t.Fatal("account not imported with activation")
	}
	state, _ := r.GetFeedSyncState(ctx, org)
	wantSourceTime := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	if state == nil || state.SourceGeneratedAt == nil || !state.SourceGeneratedAt.Equal(wantSourceTime) {
		t.Fatalf("authoritative source timestamp not persisted: %+v", state)
	}
	if state.LastSuccessAt == nil || state.LastSuccessAt.Equal(wantSourceTime) {
		t.Fatalf("sync completion and source age must remain distinct: %+v", state)
	}

	// Same snapshot → noop
	res2, xerr := svc.SyncFeedManifest(ctx, org, &user, uri)
	if xerr != nil {
		t.Fatalf("sync2: %v", xerr)
	}
	if !res2.SkippedSame || res2.Status != "noop" {
		t.Fatalf("expected noop, got %+v", res2)
	}

	// A producer retry may assign a new run ID to identical snapshot content.
	// No-op must retain the applied run ID or every account becomes non-current.
	sameSnapshot := cloneManifestMap(t, man)
	sameSnapshot["generated_at"] = "2026-08-09T10:00:00Z"
	sameSnapshot["source"] = map[string]any{
		"system": "extra-cli", "run_id": "run-sync-retry", "snapshot_hash": "snap-sync-1",
		"profile_id": "p", "profile_version": "1",
	}
	sameRaw, _ := json.Marshal(sameSnapshot)
	samePath := filepath.Join(dir, "manifest_same_snapshot.json")
	if err := os.WriteFile(samePath, sameRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	sameResult, xerr := svc.SyncFeedManifest(ctx, org, &user, "file://"+samePath)
	if xerr != nil || !sameResult.SkippedSame || sameResult.RunID != "run-sync-1" {
		t.Fatalf("same snapshot changed applied run: result=%+v err=%v", sameResult, xerr)
	}
	state, _ = r.GetFeedSyncState(ctx, org)
	if state.LastRunID != "run-sync-1" {
		t.Fatalf("same snapshot drifted authoritative run to %q", state.LastRunID)
	}

	// A different snapshot with an older authoritative timestamp is a rollback,
	// even when it is synchronized now.
	rollback := cloneManifestMap(t, man)
	rollback["generated_at"] = "2026-08-07T10:00:00Z"
	rollback["source"] = map[string]any{
		"system": "extra-cli", "run_id": "run-old", "snapshot_hash": "snap-old",
		"profile_id": "p", "profile_version": "1",
	}
	rollbackRaw, _ := json.Marshal(rollback)
	rollbackPath := filepath.Join(dir, "manifest_rollback.json")
	if err := os.WriteFile(rollbackPath, rollbackRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, xerr := svc.SyncFeedManifest(ctx, org, &user, "file://"+rollbackPath); xerr == nil {
		t.Fatal("older snapshot must be rejected")
	}

	// Corrupt hash → fail closed, no success
	manBad := man
	manBad["generated_at"] = "2026-08-10T10:00:00Z"
	manBad["source"] = map[string]any{
		"system": "extra-cli", "run_id": "run-sync-2", "snapshot_hash": "snap-sync-2",
		"profile_id": "p", "profile_version": "1",
	}
	manBad["chunks"] = []map[string]any{
		{"file": "chunk_0000.json", "chunk_index": 0, "content_hash": "deadbeef", "lead_count": 1},
	}
	manBadRaw, _ := json.MarshalIndent(manBad, "", "  ")
	manBadPath := filepath.Join(dir, "manifest_bad.json")
	_ = os.WriteFile(manBadPath, manBadRaw, 0o644)
	res3, xerr := svc.SyncFeedManifest(ctx, org, &user, "file://"+manBadPath)
	if xerr == nil {
		t.Fatal("expected hash mismatch failure")
	}
	if res3 != nil && res3.Status == "completed" {
		t.Fatal("must not complete on bad hash")
	}
}

func TestLastAppliedSnapshotNeverPromotesPartialChunkRun(t *testing.T) {
	orgID := uuid.New()
	sourceTime := time.Now().UTC().Add(-time.Hour)
	repo := newMemRepo()
	repo.feedSync = map[uuid.UUID]*models.OutreachFeedSyncState{
		orgID: {
			OrganizationID: orgID, LastSnapshotHash: "last-complete-snapshot", LastRunID: "last-complete-run",
			SourceGeneratedAt: &sourceTime, LastStatus: "partial",
		},
	}
	svc := &service{repo: repo}
	snapshot, runID := svc.lastAppliedSnapshot(context.Background(), orgID)
	if snapshot != "last-complete-snapshot" || runID != "last-complete-run" {
		t.Fatalf("partial attempt displaced last complete snapshot: snapshot=%q run=%q", snapshot, runID)
	}
}

func TestSyncFeedManifestValidatesAllChunksBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	org := uuid.New()
	user := uuid.New()
	r := newMemRepo()
	svc := NewService(Config{
		Enabled: true, DefaultDailyLimit: 100, MaxInitialEmailWords: 120,
		RequireHumanApproval: true, MaxFeedPayloadBytes: 8 << 20,
	}, r, nil).(*service)

	generatedAt := "2026-08-09T10:00:00Z"
	firstLead := sampleLeadWithActivation(70, ActivationActionableNow)
	firstLead.SourceLeadID = "prefetch-first"
	firstLead.Company.CNPJ14 = "22333444000192"
	first := Feed{
		SchemaVersion: modelsOutreachSchema(), GeneratedAt: generatedAt,
		Source:     FeedSource{System: "extra-cli", RunID: "run-prefetch", SnapshotHash: "snap-prefetch", ProfileID: "p", ProfileVersion: "1"},
		Pagination: FeedPagination{HasMore: true}, Leads: []FeedLead{firstLead},
	}
	secondLead := sampleLeadWithActivation(71, ActivationActionableNow)
	secondLead.SourceLeadID = "prefetch-second"
	secondLead.Company.CNPJ14 = "33444555000103"
	second := Feed{
		SchemaVersion: modelsOutreachSchema(), GeneratedAt: generatedAt,
		Source: first.Source, Pagination: FeedPagination{HasMore: false}, Leads: []FeedLead{secondLead},
	}
	firstRaw, _ := json.Marshal(first)
	secondRaw, _ := json.Marshal(second)
	firstSum := sha256.Sum256(firstRaw)
	if err := os.WriteFile(filepath.Join(dir, "chunk_0000.json"), firstRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunk_0001.json"), secondRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": "confenge.outreach.manifest.v1", "generated_at": generatedAt,
		"source": map[string]any{
			"system": "extra-cli", "run_id": "run-prefetch", "snapshot_hash": "snap-prefetch",
			"profile_id": "p", "profile_version": "1",
		},
		"lead_count": 2, "chunk_count": 2,
		"chunks": []map[string]any{
			{"file": "chunk_0000.json", "chunk_index": 0, "content_hash": hex.EncodeToString(firstSum[:]), "lead_count": 1, "has_more": true},
			{"file": "chunk_0001.json", "chunk_index": 1, "content_hash": "corrupt-second-hash", "lead_count": 1, "has_more": false},
		},
		"deactivations": []any{}, "deactivation_count": 0,
	}
	manifestRaw, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	res, xerr := svc.SyncFeedManifest(context.Background(), org, &user, "file://"+manifestPath)
	if xerr == nil || res == nil || res.ChunksImported != 0 {
		t.Fatalf("expected preflight failure without imports, result=%+v err=%v", res, xerr)
	}
	account, err := r.GetAccountByCNPJ(context.Background(), org, firstLead.Company.CNPJ14)
	if err != nil {
		t.Fatal(err)
	}
	if account != nil {
		t.Fatal("valid first chunk mutated state before corrupt second chunk was rejected")
	}
}

func cloneManifestMap(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func modelsOutreachSchema() string {
	// Keep in sync with models.OutreachSchemaV1 without importing cycle issues in test helper name.
	return "confenge.outreach.v1"
}
