package socket

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
)

type SocketService interface {
	GenerateWebsocketToken(ctx context.Context, userID uuid.UUID) (string, *errx.Error)
}

type socketService struct {
	cache        *cache.Cache
	tokenService token.TokenService
}

func NewService(cache *cache.Cache, tokenService token.TokenService) SocketService {
	return &socketService{
		cache:        cache,
		tokenService: tokenService,
	}
}
