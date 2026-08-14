package wmail

import (
	"context"
	"slices"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WMail) Sync(ctx context.Context) *errx.MailError {
	if w.SmtpImapData == nil || w.SmtpImapData.ImapClient == nil {
		return nil
	}

	folders, err := w.SmtpImapData.ImapClient.Folders()
	if err != nil {
		return err
	}

	for _, box := range folders {
		// SELECT is source of truth for EXISTS/UIDNEXT/HIGHESTMODSEQ. LIST-STATUS
		// alone is insufficient on Hostinger (flat MODSEQ + empty LIST counts).
		sel, serr := w.SmtpImapData.ImapClient.SelectForSyncInfo(box.Name)
		if serr != nil {
			return serr
		}

		cur := box
		cur.NumMessages = sel.NumMessages
		cur.UIDNext = sel.UIDNext
		if sel.HighestModSeq != 0 {
			cur.HighestModSeq = sel.HighestModSeq
		}
		if sel.UIDValidity != 0 {
			cur.UIDValidity = sel.UIDValidity
		}

		befBox := w.SmtpImapData.FindPair(&cur)
		plan := planMailboxSync(befBox, &cur)

		if befBox == nil {
			if err := w.mboxEvent(&cur); err != nil {
				return nil
			}
			if sel.NumMessages > 0 {
				if err := w.SmtpImapData.ImapClient.FetchChanges(ctx, 0, sel.NumMessages); err != nil {
					return err
				}
			}
			stored := cur
			w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &stored)
			continue
		}

		if plan.Fetch {
			w.SmtpImapData.mailbox = cur.UIDValidity
			if sel.NumMessages > 0 {
				if plan.Full {
					if err := w.SmtpImapData.ImapClient.FetchChanges(ctx, plan.LastModSeq, sel.NumMessages); err != nil {
						return err
					}
				} else {
					lo, hi := plan.Lo, plan.Hi
					if hi > sel.NumMessages {
						hi = sel.NumMessages
					}
					if lo < 1 {
						lo = 1
					}
					if lo <= hi {
						if err := w.SmtpImapData.ImapClient.FetchSeqWindow(ctx, plan.LastModSeq, lo, hi); err != nil {
							return err
						}
					}
				}
			}
		}

		if plan.Fetch || befBox.Name != cur.Name || !slices.Equal(befBox.Attrs, cur.Attrs) ||
			befBox.NumMessages != cur.NumMessages || befBox.UIDNext != cur.UIDNext ||
			befBox.HighestModSeq != cur.HighestModSeq {
			if plan.Fetch || befBox.Name != cur.Name || !slices.Equal(befBox.Attrs, cur.Attrs) ||
				befBox.HighestModSeq != cur.HighestModSeq {
				w.mboxEvent(&cur)
			}
			for _, ibox := range w.SmtpImapData.Mailboxes {
				if ibox.UIDValidity == cur.UIDValidity {
					ibox.HighestModSeq = cur.HighestModSeq
					ibox.NumMessages = cur.NumMessages
					ibox.UIDNext = cur.UIDNext
					ibox.Name = cur.Name
					ibox.Attrs = cur.Attrs
				}
			}
		}
	}

	var deleted []uint32
outer:
	for _, box := range w.SmtpImapData.Mailboxes {
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
		deleted = append(deleted, box.UIDValidity)
	}

	if len(deleted) > 0 {
		filtered := w.SmtpImapData.Mailboxes[:0]
		for _, b := range w.SmtpImapData.Mailboxes {
			if !slices.Contains(deleted, b.UIDValidity) {
				filtered = append(filtered, b)
			}
		}
		w.SmtpImapData.Mailboxes = filtered
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
