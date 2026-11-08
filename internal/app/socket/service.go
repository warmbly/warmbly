package socket

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
)

type SocketService interface {
	// GenerateWebsocketToken mints a short-lived socket ticket bound to the caller's session; portal for a session bound to an agency portal.
	GenerateWebsocketToken(ctx context.Context, userID, sessionID uuid.UUID, portal bool) (string, *errx.Error)
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
