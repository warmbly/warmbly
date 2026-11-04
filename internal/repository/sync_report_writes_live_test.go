package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// rowVersion is a row's xmin, which moves only when the row is rewritten.
func rowVersion(t *testing.T, f transportFixture, query string) string {
	t.Helper()
	var xmin string
	if err := f.pool.QueryRow(t.Context(), query, f.account).Scan(&xmin); err != nil {
		t.Fatal(err)
	}
	return xmin
}

func ownedTransportFixture(t *testing.T) (transportFixture, uuid.UUID) {
	t.Helper()
	f := newTransportFixture(t)
	worker := uuid.New()
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO fleet_nodes(id,role) VALUES($1,'worker')`, worker); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workers(id) VALUES($1)`, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET worker_id=$2 WHERE id=$1`, f.account, worker); err != nil {
		t.Fatal(err)
	}
	return f, worker
}

func TestLiveSyncReportWritesOnlyWhatChanged(t *testing.T) {
	f, worker := ownedTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	const stateRow = `SELECT xmin::text FROM email_sync_state WHERE email_id=$1`
	const accountRow = `SELECT xmin::text FROM email_accounts WHERE id=$1`
	synced := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Millisecond)
	state := models.SyncState{TransportGeneration: f.generation, BackfillStatus: models.SyncBackfillRunning, BackfillSynced: 3, LastSyncedAt: &synced,
		BackfillCursor: models.SyncCursor{Folders: map[string]models.SyncFolderCursor{"INBOX": {}}}}
	put := func(st models.SyncState) *SyncStatePrior {
		t.Helper()
		accepted, prior, err := repo.PutFromWorker(t.Context(), f.user, f.account, worker, f.generation, &st, nil)
		if err != nil || !accepted {
			t.Fatalf("accepted=%t err=%v", accepted, err)
		}
		return prior
	}
	if prior := put(state); prior != nil {
		t.Fatalf("first report replaced %+v", prior)
	}
	stored, account := rowVersion(t, f, stateRow), rowVersion(t, f, accountRow)

	// A redelivery of the same report rewrites nothing.
	prior := put(state)
	if prior == nil || prior.BackfillStatus != models.SyncBackfillRunning || prior.LastSyncedAt == nil || !prior.LastSyncedAt.Equal(synced) {
		t.Fatalf("prior=%+v", prior)
	}
	if rowVersion(t, f, stateRow) != stored || rowVersion(t, f, accountRow) != account {
		t.Fatal("an identical report rewrote a row")
	}

	// Half a minute later the state persists but the account's "Last synced" waits for the minute.
	soon := synced.Add(30 * time.Second)
	state.LastSyncedAt, state.BackfillSynced = &soon, 4
	put(state)
	got, err := repo.Get(t.Context(), f.account)
	if err != nil || got.BackfillSynced != 4 || !got.LastSyncedAt.Equal(soon) {
		t.Fatalf("changed report not stored: %+v %v", got, err)
	}
	if rowVersion(t, f, accountRow) != account {
		t.Fatal("account row rewritten inside the minute")
	}
	later := synced.Add(2 * time.Minute)
	state.LastSyncedAt = &later
	put(state)
	var accountAt time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT last_synced_at FROM email_accounts WHERE id=$1`, f.account).Scan(&accountAt); err != nil || !accountAt.Equal(later) {
		t.Fatalf("account last_synced_at=%v err=%v, want %v", accountAt, err, later)
	}
}

func TestLiveSyncRecoveryClockStaysExactAndSkipsOnlyStaleReports(t *testing.T) {
	f, worker := ownedTransportFixture(t)
	repo := NewEmailSyncStateRepository(f.db)
	const recoveryRow = `SELECT xmin::text FROM email_account_transport_recovery WHERE email_account_id=$1 AND operation='sync'`
	recover := func(at time.Time) func(context.Context, *SyncStatePrior) error {
		return func(ctx context.Context, _ *SyncStatePrior) error {
			if xerr := f.repo.RecordTransportSuccess(ctx, f.account, models.MailTransportSync, at, nil, f.generation); xerr != nil {
				return xerr
			}
			return nil
		}
	}
	put := func(at time.Time) {
		t.Helper()
		state := models.SyncState{TransportGeneration: f.generation, LastSyncedAt: &at}
		if accepted, _, err := repo.PutFromWorker(t.Context(), f.user, f.account, worker, f.generation, &state, recover(at)); err != nil || !accepted {
			t.Fatalf("accepted=%t err=%v", accepted, err)
		}
	}
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	put(base)
	quiet := rowVersion(t, f, recoveryRow)
	put(base.Add(-time.Second))
	if rowVersion(t, f, recoveryRow) != quiet {
		t.Fatal("a report older than the recovery clock rewrote it")
	}
	put(base.Add(20 * time.Second))
	if rowVersion(t, f, recoveryRow) == quiet {
		t.Fatal("a newer pass did not move the recovery clock, so a late error would be judged against a stale one")
	}
	// An error raised now is cleared by the very next pass.
	errAt := base.Add(30 * time.Second)
	if _, err := f.repo.CreateOnce(t.Context(), f.report(models.MailTransportSync, &errAt)); err != nil {
		t.Fatal(err)
	}
	put(base.Add(40 * time.Second))
	if open := f.errors(t, true); len(open) != 0 {
		t.Fatalf("pending error survived a later pass: %+v", open)
	}
	var recovered time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT recovered_at FROM email_account_transport_recovery WHERE email_account_id=$1 AND operation='sync'`, f.account).Scan(&recovered); err != nil || !recovered.Equal(base.Add(40*time.Second)) {
		t.Fatalf("recovered_at=%v err=%v", recovered, err)
	}
}
