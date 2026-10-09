package mailmanager

import (
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
)

func (m *MailManager) Terminate(id uuid.UUID) {
	m.Lock()
	mail := m.Emails[id]
	delete(m.Emails, id)
	m.Unlock()
	if mail != nil {
		mail.Stop()
	}
}

// TerminateMailbox cannot retire a newer generation through a stale callback.
func (m *MailManager) TerminateMailbox(id uuid.UUID, mail *wmail.WMail) {
	m.Lock()
	if m.Emails[id] == mail {
		delete(m.Emails, id)
	}
	m.Unlock()
	if mail != nil {
		mail.Stop()
	}
}
