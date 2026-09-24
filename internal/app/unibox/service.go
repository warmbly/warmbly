package unibox

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasksched"
)

type UniboxService interface {
	Incoming(
		ctx context.Context,
		userID uuid.UUID,
		limit, cursor, from string,
	) (*models.MailSearchResult, *errx.Error)
	Search(
		ctx context.Context,
		orgID uuid.UUID,
		params *models.MailSearchParams,
	) (*models.MailSearchResult, *errx.Error)
	GetByID(
		ctx context.Context,
		orgID, id uuid.UUID,
	) (*models.EmailMessage, *errx.Error)
	// ForwardSource is the message a forward carries, read without marking it
	// seen. NotFound when the id names no message in the organization.
	ForwardSource(ctx context.Context, orgID, id uuid.UUID) (*models.ForwardedMessage, *errx.Error)
	GetByThread(
		ctx context.Context,
		orgID, emailID uuid.UUID,
		threadID, limit, cursor string,
	) (*models.MailSearchResult, *errx.Error)
	// LatestMessageIDInThread is the RFC Message-ID a reply into this thread
	// should name in its In-Reply-To header. Returns "" when the thread is
	// unknown or holds no message id, which callers treat as "no backfill".
	LatestMessageIDInThread(ctx context.Context, orgID uuid.UUID, threadID string) (string, *errx.Error)
	GetUnseenCount(
		ctx context.Context,
		orgID uuid.UUID,
		emailAccountID *uuid.UUID,
	) (int64, *errx.Error)
	MarkSeen(ctx context.Context, userID, emailID uuid.UUID, seen bool) *errx.Error
	MarkSeenBulk(ctx context.Context, orgID uuid.UUID, data *models.MarkSeen) (*models.MarkSeen, *errx.Error)
	MoveFolderBulk(ctx context.Context, orgID uuid.UUID, data *models.MoveFolder) (*models.MoveFolder, *errx.Error)

	// Snooze hides conversations until `until`. Unsnooze drops the rows. Both
	// take a set so the list's selection bar is one call, not one per row.
	Snooze(ctx context.Context, userID uuid.UUID, threadIDs []string, until time.Time) ([]models.UniboxSnooze, *errx.Error)
	Unsnooze(ctx context.Context, userID uuid.UUID, threadIDs []string) *errx.Error
	ListSnoozes(ctx context.Context, userID uuid.UUID) ([]models.UniboxSnooze, *errx.Error)

	// Overview powers the scope rail + top metric strip in one call.
	Overview(ctx context.Context, orgID, userID uuid.UUID) (*models.UniboxOverview, *errx.Error)

	// Conversation labels. SetThreadLabels replaces a thread's full
	// label set (idempotent); ListThreadLabels reads the current set.
	SetThreadLabels(ctx context.Context, orgID, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) ([]models.MiniCategory, *errx.Error)
	ListThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string) ([]models.MiniCategory, *errx.Error)

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

	// ThreadGrounding and AddressGrounding return message text for AI prompts:
	// the stored body when it exists, the preview snippet as the fallback.
	ThreadGrounding(ctx context.Context, orgID uuid.UUID, threadID string, limit int) ([]models.MessageGrounding, *errx.Error)
	AddressGrounding(ctx context.Context, orgID uuid.UUID, address string, limit int) ([]models.MessageGrounding, *errx.Error)

	// StartBodyTextBackfill fills in the searchable text of messages stored
	// before bodies were indexed. Runs until the archive is caught up, then
	// returns; blocking, so callers run it in a goroutine.
	StartBodyTextBackfill(ctx context.Context)

	// WireProviderRelay attaches the worker bus, after which a read/unread
	// change made here is carried out to the mailbox provider too.
	WireProviderRelay(p events.Publisher)
}

type uniboxService struct {
	uniboxRepository repository.UniboxRepository
	taskRepo         repository.TaskRepository
	tasksClient      tasksched.Scheduler
	cache            *cache.Cache
	blob             storage.Store
	// publisher relays read/unread changes out to the mailbox providers.
	// Optional: without it the unibox still works and only Warmbly's own copy
	// of the read state changes.
	publisher events.Publisher
}

// WireProviderRelay attaches the bus the unibox relays read state through.
// Wired after construction, like the webhook dispatcher on the mailbox
// service, because a deployment without a worker bus is still a working
// unibox.
func (s *uniboxService) WireProviderRelay(p events.Publisher) {
	s.publisher = p
}

func NewService(
	cache *cache.Cache,
	blob storage.Store,
	uniboxRepository repository.UniboxRepository,
	taskRepo repository.TaskRepository,
	tasksClient tasksched.Scheduler,
) UniboxService {
	return &uniboxService{
		uniboxRepository: uniboxRepository,
		taskRepo:         taskRepo,
		tasksClient:      tasksClient,
		cache:            cache,
		blob:             blob,
	}
}
