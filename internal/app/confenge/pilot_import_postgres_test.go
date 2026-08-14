package confenge

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/repository"
)

func TestPostgresImportThenPreparePilotMembership(t *testing.T) {
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
	defer pool.Close()

	userID, orgID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email) VALUES ($1,'Pilot','Import',$2)`, userID, "pilot-import-"+userID.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1,'Pilot Import',$2,$3)`, orgID, "pilot-import-"+orgID.String(), userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
	})

	now := time.Now().UTC().Truncate(time.Millisecond)
	ready := true
	notBlocked := false
	feed := Feed{
		SchemaVersion: modelsOutreachSchema(), GeneratedAt: now.Format(time.RFC3339Nano),
		Source: FeedSource{System: "extra-cli", RunID: "pg-import-" + uuid.NewString(), SnapshotHash: "pg-import-snapshot-" + uuid.NewString(), ProfileID: "confenge", ProfileVersion: "1"},
		Leads: []FeedLead{{
			SourceLeadID:     "pg-import-lead",
			Company:          FeedCompany{CNPJ14: "76271049000185", CNPJRoot: "76271049", RazaoSocial: "Pilot Import Ltda", NomeFantasia: "Pilot Import", Website: "https://pilot.warmbly.com"},
			Priority:         FeedPriority{Rank: 1, Score: 90, Tier: "HIGH", Confidence: "HIGH"},
			Moment:           FeedMoment{Code: "CONTRACT_EXTENSION", Summary: "Prorrogação contratual publicada", ObservedAt: now.Add(-time.Hour).Format("2006-01-02"), Confidence: "HIGH", EvidenceIDs: []string{"ev-pg"}},
			Offer:            FeedOffer{ServiceCode: "ADDITIVE_REVIEW", ServiceName: "Revisão de aditivos", EntryOffer: "Leitura técnica", Rationale: "Prorrogação recente"},
			MessagingContext: FeedMessaging{FactToMention: "Prorrogação contratual publicada no portal oficial", QuestionToAsk: "Faz sentido revisar os controles?", CTA: "Posso enviar um checklist?"},
			TargetFitClass:   "TARGET_CONFIRMED", TargetFitVersion: "v1", TargetFitComputedAt: now.Format(time.RFC3339Nano), TargetFitSourceWatermark: now.Format(time.RFC3339Nano), TargetFitFresh: &ready, TargetFitSendTier: "A_AUTOMATIC", EmailSendReady: &ready,
			Contacts: []FeedContact{{SourceContactID: "pg-contact", Name: "Ana", Role: "Contratos", Email: "ana@pilot.warmbly.com", SourceURL: "https://pilot.warmbly.com/contacts/ana", SourceDate: now.Format("2006-01-02"), VerificationStatus: "OFFICIAL_SOURCE", Confidence: "HIGH", Recommended: true, EmailSendReady: &ready, MailboxPurpose: "PERSONAL_WORK", MailboxPurposeSendBlocked: &notBlocked, OwnershipStatus: "COMPANY_OWNED", RecipientCommercialSuitability: "SUITABLE", ProvenanceChainValid: &ready, DerivedFromFixture: &notBlocked}},
			Evidence: []FeedEvidence{{ID: "ev-pg", Type: "PUBLICATION", Title: "Publicação oficial", URL: "https://pilot.warmbly.com/evidence/ev-pg", Date: now.Add(-time.Hour).Format("2006-01-02"), Excerpt: "Prorrogação formal registrada.", Synthesis: "Prorrogação confirmada", EpistemicClass: "CONFIRMED_FACT", Reliability: "HIGH", ConsultedAt: now.Format("2006-01-02")}},
		}},
	}
	raw, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewOutreachRepository(pool)
	svc := NewService(Config{Enabled: true, RequireHumanApproval: true, FeedMaxAge: 24 * time.Hour, MaxFeedPayloadBytes: DefaultMaxPayloadBytes}, repo, nil).(*service)
	run, xerr := svc.ImportFromBytes(ctx, orgID, &userID, raw, ImportOptions{IdempotencyKey: "pg-import"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	account, err := repo.GetAccountByCNPJ(ctx, orgID, feed.Leads[0].Company.CNPJ14)
	if err != nil || account == nil {
		t.Fatalf("account after import: %v", err)
	}
	result, xerr := svc.PreparePilotCohort(ctx, orgID, userID, []uuid.UUID{account.ID}, PilotOperation{IdempotencyKey: "pg-prepare"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if result.Prepared != 1 {
		t.Fatalf("prepared=%d blocked=%d result=%+v import=%s", result.Prepared, result.Blocked, result.Results, run.ID)
	}
	retry, xerr := svc.PreparePilotCohort(ctx, orgID, userID, []uuid.UUID{account.ID}, PilotOperation{IdempotencyKey: "pg-prepare"})
	if xerr != nil || retry.Prepared != 1 || len(retry.Results) != 1 || !retry.Results[0].Idempotent {
		t.Fatalf("idempotent retry: result=%+v error=%v", retry, xerr)
	}
	cohortID := uuid.NewSHA1(orgID, []byte("confenge-pilot-v1")).String()
	memberships, err := repo.ListPilotMemberships(ctx, orgID, cohortID)
	if err != nil || len(memberships) != 1 {
		t.Fatalf("memberships=%d error=%v", len(memberships), err)
	}
}
