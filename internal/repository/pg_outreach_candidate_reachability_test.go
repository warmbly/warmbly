package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

func TestPostgresCandidateReachabilityRoundTrip(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	userID, orgID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email) VALUES ($1,'Reach','Scan',$2)`, userID, "reach-scan-"+userID.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1,'Reach Scan',$2,$3)`, orgID, "reach-scan-"+orgID.String(), userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
	})

	repo := NewOutreachRepository(pool)
	acc := &models.OutreachAccount{
		OrganizationID: orgID, CNPJ14: "11111000000191", QueueState: models.OutreachQueueNeedsContact,
		RazaoSocial: "Scan Ltda", NomeFantasia: "Scan",
	}
	if _, err := repo.UpsertAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}
	ready := true
	c := &models.OutreachContactCandidate{
		OrganizationID: orgID, AccountID: acc.ID, SourceContactID: "ct-r2-scan",
		Name: "Bruno Lima", Role: "Gerente de Contratos", Email: "bruno.lima@pampas.example",
		VerificationStatus: models.OutreachVerifyOfficialSource, Confidence: "MEDIUM",
		Recommended: true, EmailSendReady: ready, MailboxPurpose: "COMERCIAL",
		OwnershipStatus: "COMPANY_OWNED", RecipientCommercialSuitability: "SUITABLE",
		ReachabilityClass: "R2_HIGH_CONFIDENCE_DIRECT", RouteType: "email",
		RouteRelation: models.RouteRelBelongsToNamedPerson,
		ChannelValue:  "bruno.lima@pampas.example", ChannelDisplay: "e-mail inferido",
	}
	if _, err := repo.UpsertCandidate(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetCandidate(ctx, orgID, c.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.ReachabilityClass != "R2_HIGH_CONFIDENCE_DIRECT" || got.RouteType != "email" ||
		got.RouteRelation != models.RouteRelBelongsToNamedPerson || got.ChannelValue != "bruno.lima@pampas.example" ||
		got.ChannelDisplay != "e-mail inferido" {
		t.Fatalf("scan dropped reachability: %+v", got)
	}
	list, err := repo.ListCandidates(ctx, orgID, acc.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: n=%d err=%v", len(list), err)
	}
	if list[0].ReachabilityClass != "R2_HIGH_CONFIDENCE_DIRECT" {
		t.Fatalf("list scan dropped class: %+v", list[0])
	}
}
