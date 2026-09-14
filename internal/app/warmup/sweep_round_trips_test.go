package warmup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// countingRepo answers every sweep read with an empty result and counts the
// round trips. Any read outside this set panics through the embedded interface.
type countingRepo struct {
	repository.WarmupRepository

	calls map[string]int
	row   *models.WarmupParticipantHealth
}

func (r *countingRepo) hit(name string) { r.calls[name]++ }

func (r *countingRepo) PurgeExpiredReputationLedger(context.Context) (int64, error) {
	r.hit("PurgeExpiredReputationLedger")
	return 0, nil
}
func (r *countingRepo) GetAllParticipantAccountIDs(context.Context) ([]uuid.UUID, error) {
	r.hit("GetAllParticipantAccountIDs")
	return []uuid.UUID{r.row.EmailAccountID}, nil
}
func (r *countingRepo) GetParticipantHealthForAccount(context.Context, uuid.UUID) (*models.WarmupParticipantHealth, error) {
	r.hit("GetParticipantHealthForAccount")
	return r.row, nil
}
func (r *countingRepo) GetParticipantHealth(_ context.Context, _ uuid.UUID, poolType string) (*models.WarmupParticipantHealth, error) {
	r.hit("GetParticipantHealth:" + poolType)
	return r.row, nil
}
func (r *countingRepo) UpdateParticipantHealth(context.Context, uuid.UUID, models.WarmupHealthState, *time.Time, string, float64) error {
	r.hit("UpdateParticipantHealth")
	return nil
}
func (r *countingRepo) SumWarmupSentSince(context.Context, uuid.UUID, time.Time) (int, error) {
	r.hit("SumWarmupSentSince")
	return 0, nil
}
func (r *countingRepo) CountWarmupSpamReportsSince(context.Context, uuid.UUID, time.Time) (int, int, error) {
	r.hit("CountWarmupSpamReportsSince")
	return 0, 0, nil
}
func (r *countingRepo) CountComplaintsAndBouncesByAccount(context.Context, uuid.UUID, time.Time) (int, int, error) {
	r.hit("CountComplaintsAndBouncesByAccount")
	return 0, 0, nil
}
func (r *countingRepo) CountDeliveredByAccount(context.Context, uuid.UUID, time.Time) (int, error) {
	r.hit("CountDeliveredByAccount")
	return 0, nil
}

// The sweep runs inside a five-minute context, so round trips per mailbox are
// the ceiling on how much of the pool a pass can judge (#492). One membership
// read, four metric scans, one write, one read-back: seven per mailbox.
func TestSweepSpendsSevenRoundTripsPerMailbox(t *testing.T) {
	repo := &countingRepo{
		calls: map[string]int{},
		row:   &models.WarmupParticipantHealth{EmailAccountID: uuid.New(), PoolType: "free", HealthState: models.WarmupHealthHealthy, SpamScore: 40},
	}
	evaluated, _, xerr := NewService(repo).EvaluateAllParticipants(context.Background())
	if xerr != nil {
		t.Fatalf("sweep: %v", xerr)
	}
	if evaluated != 1 {
		t.Fatalf("evaluated %d, want 1", evaluated)
	}

	want := map[string]int{
		"PurgeExpiredReputationLedger":       1,
		"GetAllParticipantAccountIDs":        1,
		"GetParticipantHealthForAccount":     1,
		"SumWarmupSentSince":                 1,
		"CountWarmupSpamReportsSince":        1,
		"CountComplaintsAndBouncesByAccount": 1,
		"CountDeliveredByAccount":            1,
		"UpdateParticipantHealth":            1,
		"GetParticipantHealth:free":          1,
	}
	for name, n := range want {
		if repo.calls[name] != n {
			t.Errorf("%s: %d calls, want %d", name, repo.calls[name], n)
		}
	}
	for name, n := range repo.calls {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected read %s x%d", name, n)
		}
	}
}

// The score is on the participant row; re-reading it was a fourth trip to the
// same table. The decision must still see it.
func TestEvaluateReadsTheSpamScoreFromTheRow(t *testing.T) {
	row := &models.WarmupParticipantHealth{EmailAccountID: uuid.New(), PoolType: "free", HealthState: models.WarmupHealthHealthy, SpamScore: 73}
	repo := &countingRepo{calls: map[string]int{}, row: row}
	metrics, err := NewService(repo).(*service).loadMetrics(context.Background(), row.EmailAccountID, row)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.SpamScore != 73 {
		t.Fatalf("SpamScore = %d, want the row's 73", metrics.SpamScore)
	}
}
