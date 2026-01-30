package unibox

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type UniboxService interface {
	Incoming(
		ctx context.Context,
		userID uuid.UUID,
		limit, cursor, from string,
	) (*models.MailSearchResult, *errx.Error)
	Search(
		ctx context.Context,
		userID uuid.UUID,
		params *models.MailSearchParams,
	) (*models.MailSearchResult, *errx.Error)
	GetByID(
		ctx context.Context,
		userID, emailID uuid.UUID,
	) (*models.EmailMessage, *errx.Error)
	GetByThread(
		ctx context.Context,
		userID, emailID uuid.UUID,
		threadID, limit, cursor string,
	) (*models.MailSearchResult, *errx.Error)
	GetUnseenCount(
		ctx context.Context,
		userID uuid.UUID,
		emailAccountID *uuid.UUID,
	) (int64, *errx.Error)
	MarkSeen(ctx context.Context, userID, emailID uuid.UUID, seen bool) *errx.Error
	MarkSeenBulk(ctx context.Context, userID uuid.UUID, data *models.MarkSeen) (*models.MarkSeen, *errx.Error)
}

type uniboxService struct {
	uniboxRepository repository.UniboxRepository
	cache            *cache.Cache
	s3               *storage.Client
}

func NewService(cache *cache.Cache, s3 *storage.Client, uniboxRepository repository.UniboxRepository) UniboxService {
	return &uniboxService{
		uniboxRepository: uniboxRepository,
		cache:            cache,
		s3:               s3,
	}
}
