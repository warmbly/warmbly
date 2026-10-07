package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
)

func TestLiveObservationUpgradeKeepsReleasedLegacyRowsUnknown(t *testing.T) {
	dsn := os.Getenv("WARMBLY_UPGRADE_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_UPGRADE_TEST_DB must name a dedicated scratch database")
	}
	source, err := iofs.New(os.DirFS("../infrastructure/db"), "migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		t.Fatal("open scratch migration fixture failed")
	}
	defer m.Close()
	version, dirty, err := m.Version()
	if !errors.Is(err, migrate.ErrNilVersion) && (err != nil || dirty || version != 265) {
		t.Fatal("upgrade fixture requires an empty database or clean released schema 265")
	}
	if errors.Is(err, migrate.ErrNilVersion) {
		if err := m.Migrate(265); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WARMBLY_TEST_DB", dsn)
	f := newPlacementFixture(t)
	ctx, testID, internal := context.Background(), uuid.New(), uuid.New()
	f.exec(`INSERT INTO placement_tests(id,organization_id,sender_account_id,subject,status) VALUES($1,$2,$3,'Legacy test','completed')`, testID, f.org, f.sender)
	f.exec(`INSERT INTO placement_results(test_id,seed_account_id,folder,message_id) VALUES($1,$2,'inbox','<legacy-seed@example.test>')`, testID, f.seeds[0])
	f.exec(`INSERT INTO warmup_received(email_account_id,internal_id,message_id,sender_account_id,landed_spam,placed) VALUES($1,$2,'<legacy-warmup@example.test>',$3,false,true)`, f.seeds[0], internal, f.sender)
	t.Cleanup(func() { f.exec(`DELETE FROM warmup_received WHERE internal_id=$1`, internal) })
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	_, pool := liveContactDB(t)
	var unchanged bool
	if err := pool.QueryRow(ctx, `SELECT message_id='<legacy-warmup@example.test>' AND NOT landed_spam AND placed AND first_landing IS NULL AND first_folder IS NULL AND observed_at IS NULL AND evidence IS NULL FROM warmup_received WHERE internal_id=$1`, internal).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("legacy warmup evidence was inferred: %v", err)
	}
	rows, err := f.repo.ListResults(ctx, []uuid.UUID{testID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[testID]) != 1 || rows[testID][0].Folder != "inbox" || rows[testID][0].FirstFolder != nil || rows[testID][0].Evidence != nil || rows[testID][0].ObservedAt != nil {
		t.Fatal("legacy seed state or missing evidence changed")
	}
}

func TestLiveSeedObservationIsImmutableAndLateReceiptKeepsTimeout(t *testing.T) {
	f := newPlacementFixture(t)
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 266)
	ctx, now := context.Background(), time.Now().UTC()
	test, results, _ := f.newTest(f.seeds, now.Add(-3*time.Hour))
	recorder := f.repo.(interface {
		RecordProbeObservation(context.Context, uuid.UUID, string, string, []string, time.Time) error
		RecordLandingObservation(context.Context, uuid.UUID, string, []string, time.Time) error
	})
	mid := "<observed-" + test.ID.String() + "@example.test>"
	if err := f.repo.SetProbeMessageID(ctx, results[0].ID, mid); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordProbeObservation(ctx, f.seeds[0], mid, "SpAm", []string{"SPAM"}, now); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.MarkProbeSent(ctx, results[0].ID, mid, now); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordLandingObservation(ctx, results[0].ID, "inbox", nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordProbeObservation(ctx, f.seeds[0], mid, "inbox", nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	late := "<late-" + test.ID.String() + "@example.test>"
	if err := f.repo.MarkProbeSent(ctx, results[1].ID, late, now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ExpireProbes(ctx, now.Add(-2*time.Hour), now.Add(-2*time.Hour), now.Add(-6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordProbeObservation(ctx, f.seeds[1], late, "inbox", nil, now); err != nil {
		t.Fatal(err)
	}
	byTest, err := f.repo.ListResults(ctx, []uuid.UUID{test.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range byTest[test.ID] {
		if r.ID == results[0].ID && (r.Folder != "spam" || r.FirstFolder == nil || *r.FirstFolder != "SpAm" || r.SentAt == nil || r.Evidence == nil || r.Evidence.SPF != "unknown") {
			t.Fatalf("first observation overwritten or send acknowledgement lost: %+v", r)
		}
		if r.ID == results[1].ID && (r.Folder != "missing" || r.FirstFolder == nil || *r.FirstFolder != "inbox" || r.ObservedAt == nil) {
			t.Fatalf("late observation lost or deadline rewritten: %+v", r)
		}
	}
}

func TestLiveWarmupFirstObservationSurvivesRescueAndRollup(t *testing.T) {
	f := newPlacementFixture(t)
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 266)
	ctx, now, internal := context.Background(), time.Now().UTC(), uuid.New()
	f.exec(`INSERT INTO warmup_received(email_account_id,internal_id,message_id,sender_account_id) VALUES($1,$2,$3,$4)`, f.seeds[0], internal, "<first@example.test>", f.sender)
	t.Cleanup(func() {
		f.exec(`DELETE FROM warmup_received WHERE internal_id=$1`, internal)
		f.exec(`DELETE FROM warmup_placement_daily WHERE sender_account_id=$1`, f.sender)
	})
	r := NewWarmupPlacementRepository(handle)
	recorder := r.(interface {
		RecordReceiptObservation(context.Context, uuid.UUID, uuid.UUID, string, []string, time.Time) error
	})
	if err := recorder.RecordReceiptObservation(ctx, f.seeds[0], internal, "spam", []string{"SPAM"}, now); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordReceiptObservation(ctx, f.seeds[0], internal, "inbox", nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RecordPlacement(ctx, f.seeds[0], internal, "google", "google_workspace", "inbox", true); err != nil {
		t.Fatal(err)
	}
	rows, err := r.Daily(ctx, f.org, &f.sender, now.AddDate(0, 0, -1), now)
	if err != nil || len(rows) != 1 || rows[0].Inbox != 0 || rows[0].Spam != 1 || rows[0].Rescued != 1 || rows[0].Instrumented != 1 {
		t.Fatalf("immutable rollup: %+v %v", rows, err)
	}
}
