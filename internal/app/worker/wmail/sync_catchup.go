package wmail

import (
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/models"
)

// fullPassPending reports work only the full pass advances that is not done
// yet: the history import, a Graph folder's first walk or recovery or the id
// upgrade, a Gmail history recovery or missing baseline, an IMAP folder recovery.
func (w *WMail) fullPassPending() bool {
	if w.tracker == nil {
		return false
	}
	if w.tracker.state.BackfillStatus != models.SyncBackfillComplete {
		return true
	}
	switch w.EmailType {
	case models.InboxProviderGoogle:
		return w.GoogleData != nil && (w.tracker.state.BackfillCursor.GoogleRecovery != nil || w.GoogleData.LastHistoryID == 0)
	case models.InboxProviderOutlook:
		if w.GraphData == nil || w.GraphData.Client == nil {
			return false
		}
		c := w.GraphData.Client
		if w.SyncContext != nil && c.ImmutableIDMode() && !w.tracker.folder(graphIdentityCursor).Done {
			return true
		}
		for _, folder := range msgraph.TrackedFolders {
			// Archive is absent on some tenants and never gets a link there.
			if c.DeltaLinks[folder] == "" && folder != msgraph.FolderArchive || w.graphRecovering(folder) {
				return true
			}
		}
	case models.InboxProviderSMTPIMAP:
		if w.SmtpImapData == nil {
			return false
		}
		boxes := make([]models.Mailbox, 0, len(w.SmtpImapData.Mailboxes))
		for _, b := range w.SmtpImapData.Mailboxes {
			boxes = append(boxes, *b)
		}
		return w.imapRecoveryPending(boxes)
	}
	return false
}
