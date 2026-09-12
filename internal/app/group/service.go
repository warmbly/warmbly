package group

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// GroupService is one label registry (folders, tags or categories). Labels
// belong to the workspace, so every method is scoped by organization; userID
// on Create records who made it and nothing more.
type GroupService interface {
	Create(ctx context.Context, orgID, userID uuid.UUID, data *models.GroupCreate) (*models.Group, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	Move(ctx context.Context, orgID, id uuid.UUID, position int32) ([]models.Order, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, data *models.GroupUpdate) (*models.Group, *errx.Error)
	List(ctx context.Context, orgID uuid.UUID) ([]models.Group, *errx.Error)
}

type groupService struct {
	groupRepository repository.GroupRepository
}

func NewService(groupRepository repository.GroupRepository) GroupService {
	return &groupService{
		groupRepository: groupRepository,
	}
}
