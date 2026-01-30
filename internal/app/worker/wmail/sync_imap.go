package wmail

import (
	"context"
	"slices"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WMail) Sync(ctx context.Context) *errx.MailError {
	folders, err := w.SmtpImapData.ImapClient.Folders()
	if err != nil {
		return err
	}

	for _, box := range folders {
		befBox := w.SmtpImapData.FindPair(&box)
		if befBox == nil {
			if err := w.mboxEvent(&box); err != nil {
				return nil
			}

			if err := w.SmtpImapData.ImapClient.FetchChanges(ctx, 0); err != nil {
				return err
			}

			w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &box)
			continue
		}

		if befBox.HighestModSeq != box.HighestModSeq {
			w.SmtpImapData.mailbox = box.UIDValidity
			if err := w.SmtpImapData.ImapClient.FetchChanges(ctx, befBox.HighestModSeq); err != nil {
				return err
			}
		}

		if befBox.HighestModSeq != box.HighestModSeq || befBox.Name != box.Name || !slices.Equal(befBox.Attrs, box.Attrs) {
			w.mboxEvent(&box)

			for _, ibox := range w.SmtpImapData.Mailboxes {
				if ibox.UIDValidity == box.UIDValidity {
					ibox.HighestModSeq = box.HighestModSeq
					ibox.Name = box.Name
					ibox.Attrs = box.Attrs
				}
			}
		}
	}

outer:
	for i, box := range w.SmtpImapData.Mailboxes {
		for _, f := range folders {
			if box.UIDValidity == f.UIDValidity {
				continue outer
			}
		}

		if err := w.onEvent(models.JobEventTypeMailboxDelete, &models.JobEventMailboxDelete{
			UserID:      w.UserID,
			EmailID:     w.ID,
			UIDValidity: box.UIDValidity,
		}); err != nil {
			return nil
		}

		w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes[:i], w.SmtpImapData.Mailboxes[i+1:]...)
	}

	return nil
}

func (w *WMail) mboxEvent(box *models.Mailbox) error {
	return w.onEvent(models.JobEventTypeMailboxUpdate, &models.JobEventMailboxUpdate{
		UserID:  w.UserID,
		EmailID: w.ID,
		Data:    box,
	})
}

func (w *SmtpImapData) FindPair(m *models.Mailbox) *models.Mailbox {
	for _, f := range w.Mailboxes {
		if f.UIDValidity == m.UIDValidity {
			return f
		}
	}
	return nil
}
