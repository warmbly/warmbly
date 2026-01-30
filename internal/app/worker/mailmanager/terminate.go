package mailmanager

import "github.com/google/uuid"

func (m *MailManager) Terminate(id uuid.UUID) {
	m.Lock()
	defer m.Unlock()

	delete(m.Emails, id)
}
