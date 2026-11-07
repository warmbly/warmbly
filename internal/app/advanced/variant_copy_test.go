package advanced

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type variantRepo struct {
	repository.AdvancedOutreachRepository
	rows    []models.CampaignABVariant
	created *models.CreateCampaignABVariantRequest
	updated *models.UpdateCampaignABVariantRequest
}

func (r *variantRepo) ListABVariants(context.Context, uuid.UUID) ([]models.CampaignABVariant, error) {
	return r.rows, nil
}

func (r *variantRepo) CreateABVariant(_ context.Context, _ uuid.UUID, req *models.CreateCampaignABVariantRequest) (*models.CampaignABVariant, error) {
	r.created = req
	return &models.CampaignABVariant{}, nil
}

func (r *variantRepo) UpdateABVariant(_ context.Context, _, _ uuid.UUID, req *models.UpdateCampaignABVariantRequest) (*models.CampaignABVariant, error) {
	r.updated = req
	return &models.CampaignABVariant{}, nil
}

func TestVariantCopyIsCheckedUnlessItIsAStepControlArm(t *testing.T) {
	broken := "{{if .Company}}x"
	step := uuid.New()
	ctx, campaign := context.Background(), uuid.New()

	repo := &variantRepo{}
	svc := &service{repo: repo}
	if _, xerr := svc.CreateABVariant(ctx, campaign, &models.CreateCampaignABVariantRequest{Name: "ctl", SequenceID: &step, IsControl: true, BodyPlain: broken}); xerr != nil {
		t.Fatalf("a step's control arm never sends its copy: %v", xerr)
	}
	for _, req := range []*models.CreateCampaignABVariantRequest{
		{Name: "campaign control", IsControl: true, BodyPlain: broken},
		{Name: "B", SequenceID: &step, Subject: broken},
	} {
		if _, xerr := svc.CreateABVariant(ctx, campaign, req); xerr == nil || xerr.Identifier != "invalid_template" {
			t.Errorf("%s: got %v, want invalid_template", req.Name, xerr)
		}
	}

	if _, xerr := svc.CreateABVariant(ctx, campaign, &models.CreateCampaignABVariantRequest{Name: "B", Subject: "Hi {{first_name}}"}); xerr != nil || repo.created.Subject != "Hi {{.FirstName}}" {
		t.Fatalf("a bare alias should be stored canonical: %v %+v", xerr, repo.created)
	}

	id := uuid.New()
	repo.rows = []models.CampaignABVariant{{ID: id, SequenceID: &step, IsControl: true}}
	if _, xerr := svc.UpdateABVariant(ctx, campaign, id, &models.UpdateCampaignABVariantRequest{BodyPlain: &broken}); xerr != nil {
		t.Fatalf("updating a step's control arm copy: %v", xerr)
	}
	off := false
	if _, xerr := svc.UpdateABVariant(ctx, campaign, id, &models.UpdateCampaignABVariantRequest{BodyPlain: &broken, IsControl: &off}); xerr == nil {
		t.Fatal("turning the control off makes it send its copy, so the copy is checked")
	}
	// Re-activating a paused variant checks the copy it stores, which it would start sending.
	paused := uuid.New()
	repo.rows = append(repo.rows, models.CampaignABVariant{ID: paused, BodyHTML: "<p>" + broken + "</p>"})
	on := true
	if _, xerr := svc.UpdateABVariant(ctx, campaign, paused, &models.UpdateCampaignABVariantRequest{IsActive: &on}); xerr == nil || xerr.Identifier != "invalid_template" {
		t.Fatalf("re-activating a variant with broken stored copy: got %v", xerr)
	}
	weight := 70
	if _, xerr := svc.UpdateABVariant(ctx, campaign, paused, &models.UpdateCampaignABVariantRequest{Weight: &weight}); xerr != nil {
		t.Fatalf("a weight change does not touch copy: %v", xerr)
	}
	plain := "{{company_name}}"
	if _, xerr := svc.UpdateABVariant(ctx, campaign, id, &models.UpdateCampaignABVariantRequest{BodyPlain: &plain, IsControl: &off}); xerr != nil || *repo.updated.BodyPlain != "{{.Company}}" {
		t.Fatalf("a bare alias should be stored canonical on update: %v", xerr)
	}
}
