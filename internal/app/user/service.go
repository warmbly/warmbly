package user

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type UserService interface {
	SaveUser(ctx context.Context, user *models.User) *errx.Error
	GetUser(ctx context.Context, userID uuid.UUID) (*models.User, *errx.Error)
	CompleteOnboarding(ctx context.Context, userID uuid.UUID, firstName, lastName, referralSource, role, teamSize string) *errx.Error
}

type userService struct {
	cache          *cache.Cache
	userRepository repository.UserRepository
}

func NewService(userRepository repository.UserRepository, cache *cache.Cache) UserService {
	return &userService{
		userRepository: userRepository,
		cache:          cache,
	}
}
