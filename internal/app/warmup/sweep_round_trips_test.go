package warmup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// countingRepo counts the sweep's round trips; a read it does not know panics.
type countingRepo struct {
	zeroMetricsRepo

	calls map[string]int
	rows  []models.WarmupParticipantHealth
	// onMetrics runs inside the metric read, so a test can cancel mid-sweep.
	onMetrics func()
}

func (r *countingRepo) hit(name string) { r.calls[name]++ }

func (r *countingRepo) PurgeExpiredReputationLedger(context.Context) (int64, error) {
	r.hit("PurgeExpiredReputationLedger")
	return 0, nil
}
func (r *countingRepo) ListParticipantHealth(context.Context) ([]models.WarmupParticipantHealth, error) {
	r.hit("ListParticipantHealth")
	return r.rows, nil
}
func (r *countingRepo) HealthMetricCounts(ctx context.Context, id uuid.UUID, a, b time.Time) (models.WarmupHealthCounts, error) {
	r.hit("HealthMetricCounts")
	if r.onMetrics != nil {
		r.onMetrics()
	}
	return r.zeroMetricsRepo.HealthMetricCounts(ctx, id, a, b)
}
func (r *countingRepo) UpdateParticipantHealth(_ context.Context, id uuid.UUID, state models.WarmupHealthState, _ *time.Time, _ string, _ float64) (*models.WarmupParticipantHealth, error) {
	r.hit("UpdateParticipantHealth")
	return &models.WarmupParticipantHealth{EmailAccountID: id, HealthState: state}, nil
}

func healthyRows(n int) []models.WarmupParticipantHealth {
	rows := make([]models.WarmupParticipantHealth, n)
	for i := range rows {
		rows[i] = models.WarmupParticipantHealth{EmailAccountID: uuid.New(), PoolType: "free", HealthState: models.WarmupHealthHealthy}
	}
	return rows
}

// Round trips per mailbox are the ceiling on how much of the pool one
// five-minute pass can judge (#492); a new read here must be budgeted on purpose.
func TestSweepSpendsTwoRoundTripsPerMailbox(t *testing.T) {
	repo := &countingRepo{calls: map[string]int{}, rows: healthyRows(5)}
	evaluated, _, xerr := NewService(repo).EvaluateAllParticipants(context.Background())
	if xerr != nil {
		t.Fatalf("sweep: %v", xerr)
	}
	if evaluated != 5 {
		t.Fatalf("evaluated %d, want 5", evaluated)
	}
	perSweep := map[string]int{"PurgeExpiredReputationLedger": 1, "ListParticipantHealth": 1}
	perMailbox := map[string]int{"HealthMetricCounts": 5, "UpdateParticipantHealth": 5}
	for name, n := range repo.calls {
		if perSweep[name] != n && perMailbox[name] != n {
			t.Errorf("%s: %d calls, want %d per sweep or %d per mailbox", name, n, perSweep[name], perMailbox[name])
		}
	}
	total := 0
	for name, n := range repo.calls {
		if perSweep[name] == 0 {
			total += n
		}
	}
	if total != 2*5 {
		t.Fatalf("%d round trips for 5 mailboxes, budget is 2 each (#492): %v", total, repo.calls)
	}
}

// A sweep cut off by its deadline stops, reports how far it got, and returns
// an error so the run is not recorded as a success.
func TestSweepStopsAtItsDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := &countingRepo{calls: map[string]int{}, rows: healthyRows(4)}
	repo.onMetrics = func() {
		if repo.calls["HealthMetricCounts"] == 2 {
			cancel()
		}
	}
	evaluated, _, xerr := NewService(repo).EvaluateAllParticipants(ctx)
	if xerr == nil {
		t.Fatal("a cut-off sweep reported success")
	}
	if evaluated != 2 {
		t.Fatalf("evaluated %d before the deadline, want 2", evaluated)
	}
	if repo.calls["HealthMetricCounts"] != 2 {
		t.Fatalf("kept reading after the deadline: %v", repo.calls)
	}
}

// The score comes off the row in hand, and the decision must still see it.
func TestEvaluateReadsTheSpamScoreFromTheRow(t *testing.T) {
	row := &models.WarmupParticipantHealth{EmailAccountID: uuid.New(), PoolType: "free", HealthState: models.WarmupHealthHealthy, SpamScore: 73}
	repo := &countingRepo{calls: map[string]int{}, rows: []models.WarmupParticipantHealth{*row}}
	metrics, err := NewService(repo).(*service).loadMetrics(context.Background(), row.EmailAccountID, row)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.SpamScore != 73 {
		t.Fatalf("SpamScore = %d, want the row's 73", metrics.SpamScore)
	}
}
