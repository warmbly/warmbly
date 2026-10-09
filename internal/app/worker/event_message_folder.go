package worker

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// HandleMessageFolder applies an Archive, Delete or Move to inbox made in the
// unibox to the mailbox itself, then answers every message now in the
// destination with a relayed UPDATE_FOLDER so the store learns where the
// provider has it.
//
// Best-effort like HandleMessageSeen: the store is already filed, nothing is
// retried, and a message the provider refuses keeps its place there.
func (w *WorkerService) HandleMessageFolder(ctx context.Context, action models.MessageFolderAction) error {
	if len(action.Messages) == 0 || !models.FilableFolder(action.Folder) {
		return nil
	}

	mail, exists := w.loadedMailbox(ctx, action.EmailID)
	if !exists {
		log.Warn().Str("email_id", action.EmailID.String()).Msg("Mailbox not loaded; filing not relayed to the provider")
		return nil
	}
	done, ok := mail.BeginExecution()
	if !ok {
		return nil
	}
	defer done()
	ctx, cancel := mail.ExecutionContext(ctx)
	defer cancel()

	log.Info().
		Str("email_id", action.EmailID.String()).
		Str("folder", action.Folder).
		Int("messages", len(action.Messages)).
		Msg("Relaying unibox filing to the provider")

	var moved []models.JobEventFolderUpdate
	switch {
	case mail.GoogleData != nil && mail.GoogleData.Client != nil:
		moved = w.relayFolderGoogle(ctx, mail, action)
	case mail.GraphData != nil && mail.GraphData.Client != nil:
		moved = w.relayFolderGraph(ctx, mail, action)
	case mail.SmtpImapData != nil && mail.SmtpImapData.ImapClient != nil:
		moved = w.relayFolderImap(ctx, mail, action)
	default:
		log.Warn().Str("email_id", action.EmailID.String()).Msg("No mail client available to relay filing")
	}
	for i := range moved {
		update := &moved[i]
		update.UserID, update.EmailID = mail.UserID, action.EmailID
		update.Folder, update.Relayed = action.Folder, true
		if err := w.Produce(models.JobEventTypeFolderUpdate, action.EmailID.String(), update); err != nil {
			log.Warn().Err(err).Str("email_id", action.EmailID.String()).Msg("Could not report a relayed filing")
		}
	}
	return nil
}

// relayFolderGoogle files the whole batch with one batchModify. Gmail ids
// survive a label change, so the answer carries only the folder.
func (w *WorkerService) relayFolderGoogle(ctx context.Context, mail *wmail.WMail, action models.MessageFolderAction) []models.JobEventFolderUpdate {
	ids := make([]string, 0, len(action.Messages))
	refs := make([]models.MessageFolderRef, 0, len(action.Messages))
	for _, m := range action.Messages {
		if m.ProviderID != "" {
			ids = append(ids, m.ProviderID)
			refs = append(refs, m)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := mail.GoogleData.Client.MoveToFolder(ctx, ids, action.Folder); err != nil {
		log.Warn().Err(err).Str("email_id", action.EmailID.String()).Int("messages", len(ids)).
			Msg("Failed to relay filing to Gmail")
		return nil
	}
	moved := make([]models.JobEventFolderUpdate, len(refs))
	for i, ref := range refs {
		moved[i].ID = ref.ID
	}
	return moved
}

// relayFolderGraph re-keys the message map around each move: a Graph move
// changes the id and the delta reports the old one removed, which would
// otherwise delete the row.
func (w *WorkerService) relayFolderGraph(ctx context.Context, mail *wmail.WMail, action models.MessageFolderAction) []models.JobEventFolderUpdate {
	client := mail.GraphData.Client
	var moved []models.JobEventFolderUpdate
	for _, ref := range action.Messages {
		id := ref.ProviderID
		if ref.RFCMessageID != "" {
			if resolved, err := client.ResolveMessageID(ctx, ref.RFCMessageID); err == nil && resolved != "" {
				id = resolved
			}
		}
		if id == "" {
			continue
		}

		entry := repository.EmailMessageData{
			UserID: mail.UserID.String(), EmailID: action.EmailID.String(), ID: ref.ID.String(), ThreadID: ref.ThreadID,
		}
		// A move the map could not be taken off the old id for is not made:
		// the delta would report that id removed and the row would go with it.
		unmapped := false
		if repo := mail.EmailMessageMapRepository; repo != nil {
			m, err := repo.Get(ctx, mail.UserID, action.EmailID, id)
			if err != nil {
				log.Warn().Err(err).Str("email_id", action.EmailID.String()).Msg("Message map unreadable; Outlook filing not relayed")
				continue
			}
			if m != nil {
				internal, err := uuid.Parse(m.ID)
				if err == nil {
					err = repo.Del(ctx, mail.UserID, action.EmailID, id, internal)
				}
				if err != nil {
					log.Warn().Err(err).Str("email_id", action.EmailID.String()).Msg("Message map not re-keyable; Outlook filing not relayed")
					continue
				}
				entry.ID, entry.ThreadID, unmapped = m.ID, m.ThreadID, true
			}
		}

		newID, err := client.MoveToCanonical(ctx, id, action.Folder)
		if err != nil || newID == "" {
			if unmapped {
				entry.MessageID = id
				_ = mail.EmailMessageMapRepository.Add(ctx, entry)
			}
			log.Warn().Err(err).Str("email_id", action.EmailID.String()).Str("graph_id", id).
				Msg("Failed to relay filing to Outlook")
			continue
		}
		if mail.EmailMessageMapRepository != nil {
			entry.MessageID = newID
			if err := mail.EmailMessageMapRepository.Add(ctx, entry); err != nil {
				log.Warn().Err(err).Str("email_id", action.EmailID.String()).Msg("Could not re-key a relayed Outlook move")
			}
		}
		moved = append(moved, models.JobEventFolderUpdate{ID: ref.ID, ProviderID: newID})
	}
	return moved
}

// relayFolderImap locates messages by Message-ID, since the stored UID is
// stale while an earlier filing's answer is in flight, and moves each folder's
// share with one command. Messages without one stay put: their map key is
// built from folder and UID, so a moved copy would sync as a second message.
func (w *WorkerService) relayFolderImap(ctx context.Context, mail *wmail.WMail, action models.MessageFolderAction) []models.JobEventFolderUpdate {
	boxes := mail.SmtpImapData.Mailboxes
	dstBox := imapFilingFolder(boxes, action.Folder)
	dst := ""
	switch {
	case dstBox != nil:
		dst = dstBox.Name
	case action.Folder == models.FolderInbox:
		dst = "INBOX"
	case action.Folder == models.FolderArchive:
		// No archive on this server: made here, as Apple Mail and Outlook do.
		dst = "Archive"
	default:
		log.Warn().Str("email_id", action.EmailID.String()).Msg("No trash folder on this mailbox; Delete not relayed")
		return nil
	}

	refs := make([]models.MessageFolderRef, 0, len(action.Messages))
	for _, ref := range action.Messages {
		if hasRFCMessageID(ref.RFCMessageID) {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}

	client := mail.SmtpImapData.ImapClient
	var answers []models.JobEventFolderUpdate
	located := locateByMessageID(ctx, client, imapRelaySearchOrder(boxes, refs, dst), refs)

	// Grouped by the folder each message was found in; dst's share is done.
	bySource := make(map[string][]models.MessageFolderRef)
	var order []string
	for _, ref := range refs {
		at, ok := located[ref.ID]
		if !ok {
			continue
		}
		if strings.EqualFold(at.folder, dst) {
			answers = append(answers, imapLocation(boxes, ref.ID, at.folder, at.uid))
			continue
		}
		if _, ok := bySource[at.folder]; !ok {
			order = append(order, at.folder)
		}
		bySource[at.folder] = append(bySource[at.folder], ref)
	}

	for _, src := range order {
		group := bySource[src]
		uids := make([]uint32, len(group))
		for i, ref := range group {
			uids[i] = located[ref.ID].uid
		}
		moved, validity, err := client.MoveUIDs(ctx, src, dst, uids)
		if err != nil {
			log.Warn().Err(err).Str("email_id", action.EmailID.String()).Str("folder", src).Int("uids", len(uids)).
				Msg("Failed to relay filing over IMAP")
			continue
		}
		var missing []string
		for _, ref := range group {
			if moved[located[ref.ID].uid] == 0 {
				missing = append(missing, ref.RFCMessageID)
			}
		}
		found := map[string]uint32{}
		if len(missing) > 0 {
			// No COPYUID: find the moved copies by the id a move keeps.
			if f, err := client.FindUIDsByMessageIDs(ctx, dst, missing); err == nil {
				found = f
			}
		}
		for _, ref := range group {
			uid := moved[located[ref.ID].uid]
			if uid == 0 {
				uid = found[ref.RFCMessageID]
			}
			update := models.JobEventFolderUpdate{ID: ref.ID}
			// The new location travels only when all of it is known, or the
			// row would pair a UID with the wrong folder; the sync finds the
			// rest by Message-ID.
			if uid != 0 && dstBox != nil {
				if validity == 0 {
					validity = dstBox.UIDValidity
				}
				if validity != 0 {
					update.FolderPath, update.UID, update.Mailbox = dstBox.Name, uid, validity
				}
			}
			answers = append(answers, update)
		}
	}
	return answers
}

// imapPlace is where a message was found: a folder as listed, and its UID.
type imapPlace struct {
	folder string
	uid    uint32
}

// imapRelaySearchOrder is where to look for the messages, most likely first:
// the folders the store last saw them in, then the destination (already
// there), then the folders a filing moves between.
func imapRelaySearchOrder(boxes []*models.Mailbox, refs []models.MessageFolderRef, dst string) []string {
	var order []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			order = append(order, name)
		}
	}
	for _, ref := range refs {
		add(ref.FolderPath)
	}
	add(dst)
	for _, box := range []*models.Mailbox{lookupInbox(boxes), lookupArchive(boxes), lookupTrash(boxes), lookupJunk(boxes)} {
		if box != nil {
			add(box.Name)
		}
	}
	return order
}

// locateByMessageID finds each message in the first folder that holds it.
func locateByMessageID(ctx context.Context, client wmail.ImapConn, folders []string, refs []models.MessageFolderRef) map[uuid.UUID]imapPlace {
	located := make(map[uuid.UUID]imapPlace, len(refs))
	for _, folder := range folders {
		var ids []string
		for _, ref := range refs {
			if _, ok := located[ref.ID]; !ok {
				ids = append(ids, ref.RFCMessageID)
			}
		}
		if len(ids) == 0 {
			break
		}
		found, err := client.FindUIDsByMessageIDs(ctx, folder, ids)
		if err != nil {
			continue
		}
		for _, ref := range refs {
			if uid, ok := found[ref.RFCMessageID]; ok {
				if _, done := located[ref.ID]; !done {
					located[ref.ID] = imapPlace{folder: folder, uid: uid}
				}
			}
		}
	}
	return located
}

// imapLocation is the answer for a message found already in its destination.
func imapLocation(boxes []*models.Mailbox, id uuid.UUID, folder string, uid uint32) models.JobEventFolderUpdate {
	for _, box := range boxes {
		if box != nil && box.Name == folder && box.UIDValidity != 0 {
			return models.JobEventFolderUpdate{ID: id, FolderPath: box.Name, UID: uid, Mailbox: box.UIDValidity}
		}
	}
	return models.JobEventFolderUpdate{ID: id}
}

func lookupJunk(boxes []*models.Mailbox) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && imap.IsSpamMailbox(b.Name, b.Attrs) {
			return b
		}
	}
	return nil
}

// imapFilingFolder is the folder a canonical filing lands in on this server.
func imapFilingFolder(boxes []*models.Mailbox, folder string) *models.Mailbox {
	switch folder {
	case models.FolderInbox:
		return lookupInbox(boxes)
	case models.FolderArchive:
		return lookupArchive(boxes)
	case models.FolderTrash:
		return lookupTrash(boxes)
	}
	return nil
}

// hasRFCMessageID reports a real Message-ID rather than the key the sync
// builds for a message without one.
func hasRFCMessageID(id string) bool {
	return id != "" && !strings.HasPrefix(id, "no-msgid/")
}
