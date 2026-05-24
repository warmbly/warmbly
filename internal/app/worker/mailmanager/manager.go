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
	emailMessageMapRepository repository.EmailMessageMapRepository,
	cipherService cipher.CipherService,
) *MailManager {
	return &MailManager{
		Emails:                    make(map[uuid.UUID]*wmail.WMail),
		OnEvent:                   onEvent,
		cache:                     cache,
		storage:                   storage,
		emailMessageMapRepository: emailMessageMapRepository,
		cipherService:             cipherService,
	}
}

// Get returns a WMail by ID, or nil if not present
func (m *MailManager) Get(id uuid.UUID) *wmail.WMail {
	m.RLock()
	defer m.RUnlock()
	return m.Emails[id]
}

// Has returns true if the manager already has this email account
func (m *MailManager) Has(id uuid.UUID) bool {
	m.RLock()
	defer m.RUnlock()
	_, ok := m.Emails[id]
	return ok
}
