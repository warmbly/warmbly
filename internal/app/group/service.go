package group

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type GroupService interface {
	Create(ctx context.Context, userID uuid.UUID, data *models.GroupCreate) (*models.Group, *errx.Error)
	Delete(ctx context.Context, userID, id uuid.UUID) *errx.Error
	Move(ctx context.Context, userID, id uuid.UUID, position int32) ([]models.Order, *errx.Error)
	Update(ctx context.Context, userID, id uuid.UUID, data *models.GroupUpdate) (*models.Group, *errx.Error)
	List(ctx context.Context, userID uuid.UUID) ([]models.Group, *errx.Error)
}

type groupService struct {
	groupRepository repository.GroupRepository
}

func NewService(groupRepository repository.GroupRepository) GroupService {
	return &groupService{
		groupRepository: groupRepository,
	}
}
