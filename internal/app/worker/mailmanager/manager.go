package mailmanager

import (
	"sync"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

type MailManager struct {
	sync.RWMutex
	Emails                    map[uuid.UUID]*wmail.WMail
	OnEvent                   func(eventType models.JobEventType, key string, body any) error
	cache                     *cache.Cache
	storage                   storage.Store
	emailMessageMapRepository repository.EmailMessageMapRepository
	syncContextRepository     repository.SyncContextRepository
	cipherService             cipher.CipherService
	oauthInbox                *config.Oauth2Inbox
}

func NewMailManager(
	onEvent func(eventType models.JobEventType, key string, body any) error,
	cache *cache.Cache,
	storage storage.Store,
	emailMessageMapRepository repository.EmailMessageMapRepository,
	syncContextRepository repository.SyncContextRepository,
	cipherService cipher.CipherService,
	oauthInbox *config.Oauth2Inbox,
) *MailManager {
	return &MailManager{
		Emails:                    make(map[uuid.UUID]*wmail.WMail),
		OnEvent:                   onEvent,
		cache:                     cache,
		storage:                   storage,
		emailMessageMapRepository: emailMessageMapRepository,
		syncContextRepository:     syncContextRepository,
		cipherService:             cipherService,
		oauthInbox:                oauthInbox,
	}
}

// cfgFor returns the OAuth config the worker uses to refresh a provider's
// delegated token. Cfg is not carried in the AddWorkerEmail payload (avro
// excludes it), so it is rebuilt here from the worker's local oauth config.
func (m *MailManager) cfgFor(t models.InboxProvider) oauth2.Config {
	if m.oauthInbox == nil {
		return oauth2.Config{}
	}
	switch t {
	case models.InboxProviderGoogle:
		if m.oauthInbox.Google != nil {
			return *m.oauthInbox.Google
		}
	case models.InboxProviderOutlook:
		if m.oauthInbox.Outlook != nil {
			return *m.oauthInbox.Outlook
		}
	}
	return oauth2.Config{}
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
