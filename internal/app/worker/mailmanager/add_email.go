package mailmanager

import (
	"context"

	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/models"
)

func (m *MailManager) AddWMail(
	ctx context.Context,
	data *models.AddWorkerEmail,
) error {
	m.Lock()
	defer m.Unlock()

	// Cfg is avro-excluded from the payload, so rebuild it from the worker's
	// local oauth config for token refresh (no-op for smtp_imap).
	data.Cfg = m.cfgFor(data.Type)

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

	m.Emails[data.ID] = newMail

	return nil
}
