package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

func filingFloorFixture(t *testing.T) (*poolLinkFixture, *taskRepository, models.WarmupActionRequest) {
	t.Helper()
	f, r := lineageFixture(t)
	worker := uuid.New()
	for _, query := range []string{
		`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`,
		`INSERT INTO workers(id)VALUES($1)`,
	} {
		if _, err := f.pool.Exec(t.Context(), query, worker); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET worker_id=$1,test_mode='diagnostic',test_receive_enabled=true WHERE id=$2`, worker, f.recipient); err != nil {
		t.Fatal(err)
	}
	id, err := NewWarmupRecoveryRepository(f.pool).EnqueueFiling(t.Context(), models.WarmupEmailAction{EmailID: f.recipient, UserID: f.user, RFCMessageID: "<floor@example.test>", Actions: []string{models.WarmupActionFile}})
	if err != nil {
		t.Fatal(err)
	}
	return f, r, models.WarmupActionRequest{MailboxID: f.recipient, WorkerID: worker, FilingID: id.String(), Actions: []string{models.WarmupActionFile}}
}

func TestLiveWarmupFilingClaimProviderFloor(t *testing.T) {
	f, _, req := filingFloorFixture(t)
	ctx := t.Context()
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_pending_filings SET next_attempt_at=NOW()-INTERVAL '1 second',provider_retry_at=NOW()+INTERVAL '10 minutes' WHERE id=$1`, req.FilingID); err != nil {
		t.Fatal(err)
	}
	r := NewWarmupRecoveryRepository(f.pool)
	claimed, err := r.ClaimFilings(ctx, 100)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("future provider floor bypassed by expired claim lease: claims=%d err=%v", len(claimed), err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_pending_filings SET provider_retry_at=NOW()-INTERVAL '1 microsecond' WHERE id=$1`, req.FilingID); err != nil {
		t.Fatal(err)
	}
	claimed, err = r.ClaimFilings(ctx, 100)
	if err != nil || len(claimed) != 1 || claimed[0].FilingID != req.FilingID {
		t.Fatalf("expired provider floor prevented retry: claims=%d err=%v", len(claimed), err)
	}
	var next time.Time
	if err := f.pool.QueryRow(ctx, `SELECT next_attempt_at FROM warmup_pending_filings WHERE id=$1`, req.FilingID).Scan(&next); err != nil || time.Until(next) < 4*time.Minute {
		t.Fatalf("ordinary claim lease changed: next=%v err=%v", next, err)
	}
}

func TestLiveWarmupFilingDeferralIsScopedCommittedAndMonotonic(t *testing.T) {
	f, r, req := filingFloorFixture(t)
	ctx := t.Context()
	admitted, err := r.AdmitWarmupAction(ctx, req)
	filing := uuid.MustParse(req.FilingID)
	if err != nil || !admitted.FilingPending || !admitted.FilingRecovery.ValidFor(req.MailboxID, req.WorkerID, filing) || admitted.FilingRecovery.ProviderRetryAt != nil {
		t.Fatalf("unheld initial admission: %+v %v", admitted, err)
	}
	zone := time.FixedZone("UTC+02", 2*60*60)
	first := time.Now().Add(31*time.Minute + 333*time.Nanosecond).In(zone)
	in := models.WarmupFilingDeferralRequest{MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: filing, ProviderRetryAt: first}
	for i, at := range []time.Time{first, first, first.Add(-time.Minute), first.Add(time.Hour)} {
		in.ProviderRetryAt = at
		out, err := r.DeferWarmupFiling(ctx, in)
		want := first
		if i == 3 {
			want = first.Add(time.Hour)
		}
		if err != nil || !out.Persisted || !out.FilingRecovery.ValidFor(req.MailboxID, req.WorkerID, filing) || out.FilingRecovery.ProviderRetryAt == nil || out.FilingRecovery.ProviderRetryAt.Before(want) {
			t.Fatalf("unconfirmed or shortened UTC floor at %s: %+v %v", at, out, err)
		}
		var next, stored time.Time
		if err := f.pool.QueryRow(ctx, `SELECT next_attempt_at,provider_retry_at FROM warmup_pending_filings WHERE id=$1`, filing).Scan(&next, &stored); err != nil || !next.Equal(stored) || !stored.Equal(*out.FilingRecovery.ProviderRetryAt) || stored.Before(at) {
			t.Fatalf("lease or stored floor differs from committed proof: next=%s floor=%s at=%s err=%v", next, stored, at, err)
		}
	}
	out, err := r.AdmitWarmupAction(ctx, req)
	if err != nil || !out.FilingPending || !out.FilingRecovery.ValidFor(req.MailboxID, req.WorkerID, filing) || out.FilingRecovery.ProviderRetryAt.Before(first.Add(time.Hour)) {
		t.Fatalf("reassignment/restart admission lost floor: %+v %v", out, err)
	}
	duplicate, err := NewWarmupRecoveryRepository(f.pool).EnqueueFiling(ctx, models.WarmupEmailAction{EmailID: req.MailboxID, UserID: f.user, RFCMessageID: "<floor@example.test>", Actions: []string{models.WarmupActionFile}})
	if err != nil || duplicate != filing {
		t.Fatalf("duplicate enqueue changed canonical filing: %s %v", duplicate, err)
	}
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || !out.FilingPending || out.FilingRecovery.ProviderRetryAt.Before(first.Add(time.Hour)) {
		t.Fatalf("duplicate enqueue reset floor: %+v %v", out, err)
	}
	if err := NewWarmupRecoveryRepository(f.pool).CompleteFiling(ctx, req.MailboxID, filing); err != nil {
		t.Fatal(err)
	}
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || out.FilingPending {
		t.Fatalf("completed filing retained floor proof: %+v %v", out, err)
	}
	if _, err := r.DeferWarmupFiling(ctx, in); !errors.Is(err, ErrSendAdmissionDenied) {
		t.Fatalf("completed filing mutated or claimed persisted success: %v", err)
	}
}

func TestLiveWarmupFilingDeferralRefusesStaleAndForeignWork(t *testing.T) {
	for _, scenario := range []string{"foreign mailbox", "foreign worker", "mixed row", "disabled", "reassigned", "deleted", "canceled query"} {
		t.Run(scenario, func(t *testing.T) {
			f, r, req := filingFloorFixture(t)
			ctx := t.Context()
			in := models.WarmupFilingDeferralRequest{MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: uuid.MustParse(req.FilingID), ProviderRetryAt: time.Now().Add(15 * time.Minute)}
			switch scenario {
			case "foreign mailbox":
				in.MailboxID = f.sender
			case "foreign worker":
				in.WorkerID = uuid.New()
			case "mixed row":
				if _, err := f.pool.Exec(ctx, `UPDATE warmup_pending_filings SET payload=jsonb_set(payload,'{actions}',to_jsonb($2::text[])) WHERE id=$1`, in.FilingID, []string{models.WarmupActionFile, models.WarmupActionDelete}); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET test_mode='off' WHERE id=$1`, req.MailboxID); err != nil {
					t.Fatal(err)
				}
			case "reassigned":
				other := uuid.New()
				if _, err := f.pool.Exec(ctx, `INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, other); err != nil {
					t.Fatal(err)
				}
				if _, err := f.pool.Exec(ctx, `INSERT INTO workers(id)VALUES($1)`, other); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, other) })
				if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, other, req.MailboxID); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if _, err := f.pool.Exec(ctx, `DELETE FROM email_accounts WHERE id=$1`, req.MailboxID); err != nil {
					t.Fatal(err)
				}
			case "canceled query":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := r.DeferWarmupFiling(ctx, in)
			if err == nil || out.Persisted || out.FilingRecovery != nil {
				t.Fatalf("unowned/unknown work claimed durable: %+v %v", out, err)
			}
			if scenario == "canceled query" && !errors.Is(err, context.Canceled) {
				t.Fatalf("database cancellation swallowed: %v", err)
			}
			var stored *time.Time
			if err := f.pool.QueryRow(t.Context(), `SELECT provider_retry_at FROM warmup_pending_filings WHERE id=$1`, in.FilingID).Scan(&stored); scenario != "deleted" && (err != nil || stored != nil) {
				t.Fatalf("refused deferral updated row: at=%v err=%v", stored, err)
			}
		})
	}
}

func TestLiveWarmupFilingProviderFloorSurvivesReassignment(t *testing.T) {
	f, r, req := filingFloorFixture(t)
	ctx := t.Context()
	filing := uuid.MustParse(req.FilingID)
	at := time.Now().Add(45 * time.Minute)
	deferral := models.WarmupFilingDeferralRequest{MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: filing, ProviderRetryAt: at}
	if out, err := r.DeferWarmupFiling(ctx, deferral); err != nil || !out.Persisted {
		t.Fatal("original worker failed to persist the floor", out, err)
	}
	other := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO workers(id)VALUES($1)`, other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, other) })
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, other, req.MailboxID); err != nil {
		t.Fatal(err)
	}
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || out.FilingPending || out.FilingRecovery != nil {
		t.Fatalf("stale worker retained scoped floor proof: %+v %v", out, err)
	}
	if _, err := r.DeferWarmupFiling(ctx, deferral); !errors.Is(err, ErrSendAdmissionDenied) {
		t.Fatalf("stale worker extended floor: %v", err)
	}
	req.WorkerID, deferral.WorkerID = other, other
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || !out.FilingPending || !out.FilingRecovery.ValidFor(req.MailboxID, other, filing) || out.FilingRecovery.ProviderRetryAt == nil || out.FilingRecovery.ProviderRetryAt.Before(at) {
		t.Fatalf("new worker lost persisted provider floor: %+v %v", out, err)
	}
	if out, err := r.DeferWarmupFiling(ctx, deferral); err != nil || !out.Persisted || !out.FilingRecovery.ValidFor(req.MailboxID, other, filing) {
		t.Fatalf("new worker cannot idempotently persist existing floor: %+v %v", out, err)
	}
}

func TestLiveWarmupFilingProviderFloorDoesNotShortenClaimLease(t *testing.T) {
	f, r, req := filingFloorFixture(t)
	ctx := t.Context()
	filing := uuid.MustParse(req.FilingID)
	var prior time.Time
	if err := f.pool.QueryRow(ctx, `UPDATE warmup_pending_filings SET next_attempt_at=NOW()+INTERVAL '3 hours' WHERE id=$1 RETURNING next_attempt_at`, filing).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	requested := time.Now().Add(20 * time.Minute)
	if out, err := r.DeferWarmupFiling(ctx, models.WarmupFilingDeferralRequest{MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: filing, ProviderRetryAt: requested}); err != nil || !out.Persisted || out.FilingRecovery.ProviderRetryAt.Before(requested) {
		t.Fatalf("floor below ordinary lease not persisted: %+v %v", out, err)
	}
	var next time.Time
	if err := f.pool.QueryRow(ctx, `SELECT next_attempt_at FROM warmup_pending_filings WHERE id=$1`, filing).Scan(&next); err != nil || !next.Equal(prior) {
		t.Fatalf("provider floor shortened ordinary lease: prior=%s next=%s err=%v", prior, next, err)
	}
}

func TestLiveWarmupFilingDeferralDatabaseErrorIsNotProof(t *testing.T) {
	f, _, req := filingFloorFixture(t)
	config := f.pool.Config()
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	readOnly, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	r := NewTaskRepository(readOnly).(*taskRepository)
	out, err := r.DeferWarmupFiling(t.Context(), models.WarmupFilingDeferralRequest{MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: uuid.MustParse(req.FilingID), ProviderRetryAt: time.Now().Add(time.Hour)})
	var databaseErr *pgconn.PgError
	if !errors.As(err, &databaseErr) || out.Persisted || out.FilingRecovery != nil {
		t.Fatalf("database error produced durable proof: %+v %v", out, err)
	}
	var floor *time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT provider_retry_at FROM warmup_pending_filings WHERE id=$1`, req.FilingID).Scan(&floor); err != nil || floor != nil {
		t.Fatalf("failed transaction retained a floor: %v %v", floor, err)
	}
}

func TestLiveWarmupFilingDeferralLostHTTPResponseRetriesIdempotently(t *testing.T) {
	f, r, req := filingFloorFixture(t)
	in := models.WarmupFilingDeferralRequest{MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: uuid.MustParse(req.FilingID), ProviderRetryAt: time.Now().Add(10 * time.Minute)}
	lost := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input models.WarmupFilingDeferralRequest
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&input) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		out, err := r.DeferWarmupFiling(request.Context(), input)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if lost {
			lost = false
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	client, err := NewHTTPSyncContextRepository(srv.URL, "test")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := client.(WarmupFilingDeferralAuthority).DeferWarmupFiling(t.Context(), in); err == nil || out.Persisted || out.FilingRecovery != nil {
		t.Fatalf("lost response treated as confirmed deferral: %+v %v", out, err)
	}
	var stored time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT provider_retry_at FROM warmup_pending_filings WHERE id=$1`, req.FilingID).Scan(&stored); err != nil || stored.Before(in.ProviderRetryAt) {
		t.Fatalf("lost response did not leave committed floor: %s %v", stored, err)
	}
	if out, err := client.(WarmupFilingDeferralAuthority).DeferWarmupFiling(t.Context(), in); err != nil || !out.Persisted || !out.FilingRecovery.ProviderRetryAt.Equal(stored) {
		t.Fatalf("retry shifted existing floor: %+v %v", out, err)
	}
}
