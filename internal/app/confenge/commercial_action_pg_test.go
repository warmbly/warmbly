package confenge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Import writes reachability onto candidates; CollectToday re-plans from
// ListCandidates (the real PG scan). R2 must stay inferred, never VALIDATED.
func TestPostgresR2InferredEmailSurvivesCandidateScan(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	userID, orgID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email) VALUES ($1,'R2','Scan',$2)`, userID, "r2-scan-"+userID.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1,'R2 Scan',$2,$3)`, orgID, "r2-scan-"+orgID.String(), userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
	})

	raw, err := os.ReadFile(filepath.Join("testdata", "reachability_r0_r5_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewOutreachRepository(pool)
	svc := NewService(Config{Enabled: true, RequireHumanApproval: true, MaxFeedPayloadBytes: DefaultMaxPayloadBytes}, repo, nil).(*service)
	if _, xerr := svc.ImportFromBytes(ctx, orgID, &userID, raw, ImportOptions{IdempotencyKey: "r2-pg-scan"}); xerr != nil {
		t.Fatal(xerr)
	}
	acc, err := repo.GetAccountByCNPJ(ctx, orgID, "22222000000182")
	if err != nil || acc == nil {
		t.Fatalf("r2 account: %v", err)
	}
	cands, err := repo.ListCandidates(ctx, orgID, acc.ID)
	if err != nil || len(cands) == 0 {
		t.Fatalf("list candidates: n=%d err=%v", len(cands), err)
	}
	if cands[0].ReachabilityClass != "R2_HIGH_CONFIDENCE_DIRECT" {
		t.Fatalf("PG scan lost R2 class: %+v", cands[0])
	}
	planned := PlanCommercialAction(PlanInput{
		Account: acc, Candidate: &cands[0], Candidates: cands, Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC),
	})
	if planned.Action.ActionType != models.ActionInferredEmailReview || planned.Action.Lane != models.LaneHumanReviewEmail {
		t.Fatalf("replan after scan: %+v", planned.Action)
	}
	if planned.Action.EmailSendable || planned.Action.Dispatchable || planned.RecipientState == RecipientValidated {
		t.Fatalf("R2 after PG reload must not be VALIDATED/sendable: %+v rec=%s", planned.Action, planned.RecipientState)
	}
}
