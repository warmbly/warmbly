package group

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *groupService) Create(ctx context.Context, orgID, userID uuid.UUID, data *models.GroupCreate) (*models.Group, *errx.Error) {
	return s.groupRepository.Create(ctx, orgID, userID, data)
}

func (s *groupService) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	return s.groupRepository.Delete(ctx, orgID, id)
}

func (s *groupService) Move(ctx context.Context, orgID, id uuid.UUID, position int32) ([]models.Order, *errx.Error) {
	return s.groupRepository.Move(ctx, orgID, id, position)
}

func (s *groupService) Update(ctx context.Context, orgID, id uuid.UUID, data *models.GroupUpdate) (*models.Group, *errx.Error) {
	return s.groupRepository.Update(ctx, orgID, id, data)
}

func (s *groupService) List(ctx context.Context, orgID uuid.UUID) ([]models.Group, *errx.Error) {
	return s.groupRepository.List(ctx, orgID)
}
