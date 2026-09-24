package mailmanager

import (
	"context"
	"errors"

	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/models"
)

// errBrokerUnavailable: a managed mailbox needs the backend's token broker.
var errBrokerUnavailable = errors.New("cloud-managed mailbox needs ENCRYPTED_KEYS_BACKEND_URL and ENCRYPTED_KEYS_WORKER_TOKEN for brokered tokens")

func (m *MailManager) AddWMail(
	ctx context.Context,
	data *models.AddWorkerEmail,
) error {
	// Cfg is avro-excluded from the payload, so rebuild it from the worker's
	// local oauth config for token refresh (no-op for smtp_imap).
	data.Cfg = m.cfgFor(data.Type)
	if data.Brokered {
		if m.tokenBroker == nil {
			return errBrokerUnavailable
		}
		data.TokenSource = m.tokenBroker.Source(data.ID)
	}

	newMail, err := wmail.NewWMail(
		data,
		m.OnEvent,
		func() {
			m.Terminate(data.ID)
		},
		m.cache,
		m.storage,
		m.emailMessageMapRepository,
		m.syncContextRepository,
		m.cipherService,
	)
	if err != nil {
		// Surface it: swallowing this leaves the mailbox absent from the map
		// while the caller logs a successful load, so a mailbox that cannot
		// authenticate looks connected and silently never sends or syncs.
		return err
	}

	// Built before the lock: NewWMail dials the server, and holding the lock
	// through that stalled every send and lookup on this worker.
	m.Lock()
	defer m.Unlock()
	if _, loaded := m.Emails[data.ID]; loaded {
		newMail.Discard()
		return nil
	}
	m.Emails[data.ID] = newMail

	return nil
}
