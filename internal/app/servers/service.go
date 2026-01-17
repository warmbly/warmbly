package servers

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/role"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type ServersService interface {
	GetWorkers(ctx context.Context, userID uuid.UUID) ([]models.Worker, *errx.Error)
}

type serversService struct {
	serversRepository repository.ServersRepository
	userService       user.UserService
	roleService       role.RoleService
	cache             *cache.Cache
}

func NewService(
	cache *cache.Cache,
	serversRepository repository.ServersRepository,
	userService user.UserService,
	roleService role.RoleService,
) ServersService {
	return &serversService{
		serversRepository: serversRepository,
		cache:             cache,
		userService:       userService,
		roleService:       roleService,
	}
}
