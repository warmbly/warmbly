package mailmanager

import (
	"sync"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type MailManager struct {
	sync.RWMutex
	Emails                    map[uuid.UUID]*wmail.WMail
	OnEvent                   func(eventType models.JobEventType, key string, body any) error
	cache                     *cache.Cache
	storage                   *storage.Client
	emailMessageMapRepository repository.EmailMessageMapRepository
	cipherService             cipher.CipherService
}

func NewMailManager(
	onEvent func(eventType models.JobEventType, key string, body any) error,
	cache *cache.Cache,
	storage *storage.Client,
) *MailManager {
	return &MailManager{
		Emails:  make(map[uuid.UUID]*wmail.WMail),
		OnEvent: onEvent,
		cache:   cache,
		storage: storage,
	}
}
