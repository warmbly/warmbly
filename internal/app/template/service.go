package template

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type TemplateService interface {
	Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateReplyTemplate) (*models.ReplyTemplate, *errx.Error)
	GetByID(ctx context.Context, orgID, templateID uuid.UUID) (*models.ReplyTemplate, *errx.Error)
	List(ctx context.Context, orgID uuid.UUID) ([]models.ReplyTemplate, *errx.Error)
	Update(ctx context.Context, orgID, templateID uuid.UUID, data *models.UpdateReplyTemplate) (*models.ReplyTemplate, *errx.Error)
	Delete(ctx context.Context, orgID, templateID uuid.UUID) *errx.Error
}

type templateService struct {
	repo repository.TemplateRepository
}

func NewService(repo repository.TemplateRepository) TemplateService {
	return &templateService{repo: repo}
}

func (s *templateService) Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateReplyTemplate) (*models.ReplyTemplate, *errx.Error) {
	if data.Name == "" {
		return nil, errx.New(errx.BadRequest, "name is required")
	}
	if len(data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "name must be at most 255 characters")
	}

	t, err := s.repo.Create(ctx, orgID, userID, data)
	if err != nil {
		return nil, errx.InternalError()
	}

	return t, nil
}

func (s *templateService) GetByID(ctx context.Context, orgID, templateID uuid.UUID) (*models.ReplyTemplate, *errx.Error) {
	t, err := s.repo.GetByID(ctx, orgID, templateID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if t == nil {
		return nil, errx.ErrNotFound
	}

	return t, nil
}

func (s *templateService) List(ctx context.Context, orgID uuid.UUID) ([]models.ReplyTemplate, *errx.Error) {
	templates, err := s.repo.List(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}

	if templates == nil {
		templates = []models.ReplyTemplate{}
	}

	return templates, nil
}

func (s *templateService) Update(ctx context.Context, orgID, templateID uuid.UUID, data *models.UpdateReplyTemplate) (*models.ReplyTemplate, *errx.Error) {
	if data.Name != nil && len(*data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "name must be at most 255 characters")
	}

	t, err := s.repo.Update(ctx, orgID, templateID, data)
	if err != nil {
		return nil, errx.InternalError()
	}
	if t == nil {
		return nil, errx.ErrNotFound
	}

	return t, nil
}

func (s *templateService) Delete(ctx context.Context, orgID, templateID uuid.UUID) *errx.Error {
	if err := s.repo.Delete(ctx, orgID, templateID); err != nil {
		return errx.InternalError()
	}

	return nil
}
