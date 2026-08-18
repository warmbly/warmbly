package worker

import (
	"context"
	"sync"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/worker/mailmanager"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type WorkerService struct {
	ID                        string
	CipherService             cipher.CipherService
	Bus                       eventbus.EventBus
	Codec                     codec.Codec
	QueueURL                  string
	Cache                     *cache.Cache
	Storage                   storage.Store
	EmailMessageMapRepository repository.EmailMessageMapRepository
	// SyncContextRepository answers the sync governor's priority-lane lookup
	// over the internal API. Optional: nil means every new message is live.
	SyncContextRepository repository.SyncContextRepository

	// OauthInbox supplies the provider OAuth configs (client id/secret +
	// endpoint) the worker needs to refresh delegated tokens locally. Cfg is not
	// serialized in the AddWorkerEmail payload, so the worker rebuilds it here.
	OauthInbox *config.Oauth2Inbox

	mailManager *mailmanager.MailManager

	// HealthCounters tracks the per-window send-side telemetry the worker
	// reports via JobEventTypeWorkerHealth. Lazily initialised by
	// ensureCounters so accessors are safe to call before RunHealth has
	// started.
	HealthCounters *HealthCounters
	healthOnce     sync.Once

	errorEvents   chan zapcore.Entry
	logger        *zap.Logger
	eventHandlers map[models.WorkerEventType]func(ctx context.Context, body any) error
}

func (s *WorkerService) Init() error {
	s.errorEvents = make(chan zapcore.Entry)
	var err error
	s.logger, err = NewLoggerWithHandler(s.HandleError)
	if err != nil {
		return err
	}

	s.mailManager = mailmanager.NewMailManager(
		s.Produce,
		s.Cache,
		s.Storage,
		s.EmailMessageMapRepository,
		s.SyncContextRepository,
		s.CipherService,
		s.OauthInbox,
	)

	return nil
}
