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

	newMail, err := wmail.NewWMail(
		data,
		m.OnEvent,
		func() {
			m.Terminate(data.ID)
		},
		m.cache,
		m.storage,
		m.emailMessageMapRepository,
		m.cipherService,
	)
	if err != nil {
		return nil
	}

	m.Emails[data.ID] = newMail

	return nil
}
