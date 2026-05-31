package unibox

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
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

	// Snooze hides a thread until `until`. Unsnooze drops the row.
	Snooze(ctx context.Context, userID uuid.UUID, threadID string, until time.Time) (*models.UniboxSnooze, *errx.Error)
	Unsnooze(ctx context.Context, userID uuid.UUID, threadID string) *errx.Error
	ListSnoozes(ctx context.Context, userID uuid.UUID) ([]models.UniboxSnooze, *errx.Error)

	// Overview powers the scope rail + top metric strip in one call.
	Overview(ctx context.Context, userID uuid.UUID) (*models.UniboxOverview, *errx.Error)

	// Scheduled-sends review + cancel. CancelScheduled is DB-only: we
	// flip status to 'cancelled' and let the queued Cloud Task fire as
	// a no-op (handler short-circuits on non-pending status). Avoids
	// per-cancel API calls against Cloud Tasks.
	ListScheduled(ctx context.Context, userID uuid.UUID) ([]models.UniboxScheduledItem, *errx.Error)
	// ListScheduledByThread returns the user's pending queued sends
	// for a single thread. ThreadView calls this so queued replies
	// render inline alongside already-sent messages.
	ListScheduledByThread(ctx context.Context, userID uuid.UUID, threadID string) ([]models.UniboxScheduledItem, *errx.Error)
	CancelScheduled(ctx context.Context, userID, taskID uuid.UUID) *errx.Error
}

type uniboxService struct {
	uniboxRepository repository.UniboxRepository
	taskRepo         repository.TaskRepository
	tasksClient      *gtasks.Client
	cache            *cache.Cache
	blob             storage.Store
}

func NewService(
	cache *cache.Cache,
	blob storage.Store,
	uniboxRepository repository.UniboxRepository,
	taskRepo repository.TaskRepository,
	tasksClient *gtasks.Client,
) UniboxService {
	return &uniboxService{
		uniboxRepository: uniboxRepository,
		taskRepo:         taskRepo,
		tasksClient:      tasksClient,
		cache:            cache,
		blob:             blob,
	}
}
