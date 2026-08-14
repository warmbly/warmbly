package confenge

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type mockCRM struct {
	pipelines []models.Pipeline
	creates   int
	tasks     int
	deals     int
}

func (m *mockCRM) ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, *errx.Error) {
	return m.pipelines, nil
}

func (m *mockCRM) CreatePipeline(ctx context.Context, orgID uuid.UUID, data *models.CreatePipeline) (*models.Pipeline, *errx.Error) {
	m.creates++
	if data.Name != DefaultPipelineName {
		return nil, errx.New(errx.BadRequest, "name")
	}
	if len(data.Stages) != 12 {
		return nil, errx.New(errx.BadRequest, "want 12 stages")
	}
	stages := make([]models.PipelineStage, len(data.Stages))
	for i, s := range data.Stages {
		stages[i] = models.PipelineStage{ID: uuid.New(), Name: s.Name, Color: s.Color, Position: i}
	}
	p := models.Pipeline{ID: uuid.New(), OrganizationID: orgID, Name: data.Name, Stages: stages}
	m.pipelines = append(m.pipelines, p)
	return &p, nil
}

func (m *mockCRM) CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, *errx.Error) {
	m.tasks++
	return &models.CRMTask{ID: uuid.New(), Title: data.Title}, nil
}

func (m *mockCRM) CreateDeal(ctx context.Context, orgID uuid.UUID, data *models.CreateDeal) (*models.Deal, *errx.Error) {
	m.deals++
	return &models.Deal{ID: uuid.New(), Name: data.Name}, nil
}

func TestBootstrapPipelineIdempotent(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	crm := &mockCRM{}
	svc.WireCRM(crm)
	org := uuid.New()
	p1, xerr := svc.BootstrapPipeline(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	p2, xerr := svc.BootstrapPipeline(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if p1.ID != p2.ID {
		t.Fatal("not idempotent")
	}
	if crm.creates != 1 {
		t.Fatalf("creates=%d", crm.creates)
	}
}

func TestClassifyReplyForCRMNeverAutoWon(t *testing.T) {
	for _, c := range []string{
		replyclassify.ClassPositive, "meeting", "proposal", "won", "WON",
	} {
		a := ClassifyReplyForCRM(c)
		if a.OutcomeType == OutcomeWon {
			t.Fatalf("%s mapped to WON", c)
		}
	}
}

func TestHandleClassifiedReplyPositiveCreatesTaskAndDeal(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	crm := &mockCRM{}
	svc.WireCRM(crm)
	org := uuid.New()
	accID := uuid.New()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "11222333000181",
		RazaoSocial: "ACME", QueueState: models.OutreachQueueEnrolled, SourceLeadID: "L1",
	}
	_, _ = r.UpsertAccount(context.Background(), acc)
	contactID := uuid.New()
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Email: "ana@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		WarmblyContactID: &contactID,
	}
	_, _ = r.UpsertCandidate(context.Background(), cand)

	if xerr := svc.HandleClassifiedReply(context.Background(), org, uuid.New(), "ana@example.com", replyclassify.ClassPositive, &contactID); xerr != nil {
		t.Fatal(xerr)
	}
	if crm.tasks < 1 {
		t.Fatal("expected task")
	}
	if crm.deals < 1 {
		t.Fatal("expected deal in Respondeu")
	}
}

func TestHandleClassifiedReplyDNC(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	crm := &mockCRM{}
	svc.WireCRM(crm)
	org := uuid.New()
	accID := uuid.New()
	_, _ = r.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "11222333000181", RazaoSocial: "X",
	})
	_, _ = r.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, Email: "x@example.com",
		VerificationStatus: models.OutreachVerifyOfficialSource,
	})
	_ = svc.HandleClassifiedReply(context.Background(), org, uuid.Nil, "x@example.com", replyclassify.ClassUnsubscribe, nil)
	acc, _ := r.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if acc == nil || !acc.DoNotContact {
		t.Fatalf("DNC not set: %+v", acc)
	}
}
