package role

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type RoleService interface {
	Create(ctx context.Context, adminID uuid.UUID, data *models.CreateRole) (*models.Role, *errx.Error)
	Get(ctx context.Context) ([]models.Role, *errx.Error)
	Update(ctx context.Context, adminID, roleID uuid.UUID, data *models.UpdateRole) (*models.Role, *errx.Error)
	Delete(ctx context.Context, adminID, roleID uuid.UUID) *errx.Error

	Add(ctx context.Context, adminID, userID, roleID uuid.UUID) *errx.Error
	Remove(ctx context.Context, adminID, userID, roleID uuid.UUID) *errx.Error

	HavePermission(ctx context.Context, roles []uuid.UUID, permissions []uint8) *errx.Error
}

type roleService struct {
	roleRepository repository.RoleRepository
	cache          *cache.Cache
}

func NewService(cache *cache.Cache, roleRepository repository.RoleRepository) RoleService {
	return &roleService{
		roleRepository: roleRepository,
		cache:          cache,
	}
}
