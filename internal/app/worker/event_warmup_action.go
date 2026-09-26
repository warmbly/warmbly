package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// HandleWarmupAction executes recipient-side warmup actions on the mailbox.
//
// The worker just runs whatever actions it receives, immediately. The
// recipient-side "dwell" and the immediate-vs-delayed split are now owned by the
// CONSUMER's durable schedule (internal/app/consumer/warmup_engagement_poller):
// the consumer publishes the immediate leg (folder + spam-rescue) right away and
// the delayed leg (read / important / star) when its fire_at passes, each with
// DelaySeconds=0. That makes the dwell survive a worker restart, which the old
// in-process time.AfterFunc here could not.
func (w *WorkerService) HandleWarmupAction(ctx context.Context, action models.WarmupEmailAction) error {
	log.Info().
		Str("email_id", action.EmailID.String()).
		Str("gmail_id", action.GmailID).
		Uint32("uid", action.UID).
		Uint32("mailbox_uid_validity", action.MailboxUIDValidity).
		Strs("actions", action.Actions).
		Msg("Processing warmup email action")

	if len(action.Actions) == 0 {
		return nil
	}

	mail, exists := w.loadedMailbox(ctx, action.EmailID)
	var err error
	switch {
	case !exists:
		// Engagement on a mailbox this worker is not holding is dropped; it
		// is best effort and a mailbox mid-move earns its signal elsewhere.
		// A retention delete is redelivered instead, because the control
		// plane has already retired the row and will not send it again.
		log.Warn().Str("email_id", action.EmailID.String()).Msg("Email account not found for warmup action")
		if hasWarmupAction(action.Actions, models.WarmupActionDelete) {
			err = errors.New("mailbox not loaded on this worker")
		}
	case mail.GoogleData != nil && mail.GoogleData.Client != nil:
		err = w.runGoogleWarmupActions(ctx, mail, action)
	case mail.GraphData != nil && mail.GraphData.Client != nil:
		err = w.runGraphWarmupActions(ctx, mail, action)
	case mail.SmtpImapData != nil && mail.SmtpImapData.ImapClient != nil:
		err = w.runImapWarmupActions(ctx, mail, action)
	default:
		log.Warn().
			Str("email_id", action.EmailID.String()).
			Msg("No mail client available for warmup actions; skipping")
	}
	if err == nil {
		return nil
	}
	// Engagement is best effort and never returns here. A retention delete
	// is not: the control plane has already retired the row, so a failure
	// here is the only chance the message has of going. The bus redelivers
	// on an error, bounded so a message the provider will never give up is
	// not retried forever.
	if d := deliveryOf(ctx); d.redelivers && d.attempt < warmupDeleteRedeliveries {
		return err
	}
	log.Error().Err(err).
		Str("email_id", action.EmailID.String()).
		Str("rfc_message_id", action.RFCMessageID).
		Msg("Warmup delete gave up; the message stays in the mailbox")
	return nil
}

// warmupDeleteRedeliveries bounds how many times a failed retention delete is
// redelivered before the message is left in place.
const warmupDeleteRedeliveries = 5

func (w *WorkerService) runGoogleWarmupActions(ctx context.Context, mail *wmail.WMail, action models.WarmupEmailAction) error {
	placement, folder := warmupFiling(action)
	// The archive placement wants the message out of every view without a label
	// of its own, which on Gmail is "remove INBOX, add nothing".
	label := folder
	if placement == models.WarmupPlacementArchive {
		label = ""
	}

	// The delete for the sender's own copy carries no Gmail id: the map is
	// keyed by the provider's id and the control plane only knows the
	// Message-ID, so it is resolved here.
	gmailID := action.GmailID
	if gmailID == "" && action.RFCMessageID != "" && hasWarmupAction(action.Actions, models.WarmupActionDelete) {
		id, err := mail.GoogleData.Client.FindByRFCMessageID(ctx, action.RFCMessageID)
		if err != nil {
			return fmt.Errorf("search Gmail for the warmup message: %w", err)
		}
		gmailID = id
	}

	for _, act := range action.Actions {
		switch act {
		case models.WarmupActionFile:
			if err := mail.GoogleData.Client.FileWarmup(ctx, action.GmailID, label); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Str("folder", label).Msg("Failed to file warmup message (Gmail)")
			}
		case models.WarmupActionDelete:
			if gmailID != "" {
				if err := mail.GoogleData.Client.Trash(ctx, gmailID); err != nil {
					return fmt.Errorf("delete warmup message (Gmail): %w", err)
				}
			}
			if err := w.dropWarmupBody(ctx, mail, action, gmailID); err != nil {
				return err
			}
		case models.WarmupActionMarkRead:
			if err := mail.GoogleData.Client.MarkAsRead(ctx, action.GmailID); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to mark as read")
			}
		case models.WarmupActionRescueFromSpam:
			toInbox := placement == models.WarmupPlacementInbox
			if err := mail.GoogleData.Client.RescueFromSpam(ctx, action.GmailID, label, toInbox); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to rescue from spam (Gmail)")
			}
		case models.WarmupActionMarkImportant:
			if err := mail.GoogleData.Client.MarkImportant(ctx, action.GmailID); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to mark important")
			}
		case models.WarmupActionStar:
			if err := mail.GoogleData.Client.AddStar(ctx, action.GmailID); err != nil {
				log.Error().Err(err).Str("gmail_id", action.GmailID).Msg("Failed to star warmup message")
			}
		default:
			log.Warn().Str("action", act).Msg("Unknown warmup action")
		}
	}
	return nil
}

// runGraphWarmupActions applies recipient-side warmup engagement to a Microsoft
// Graph mailbox. action.GmailID carries the Graph message id (the provider id
// field is provider-agnostic). Star maps to the follow-up flag, the closest
// Outlook equivalent of a Gmail star.
func (w *WorkerService) runGraphWarmupActions(ctx context.Context, mail *wmail.WMail, action models.WarmupEmailAction) error {
	client := mail.GraphData.Client

	// A Graph message id changes whenever the message is moved (copy+delete), so
	// resolve the live id from the immutable RFC Message-ID before acting. This
	// leg may run after an earlier leg already moved the message to Warmbly.
	msgID := action.GmailID
	if action.RFCMessageID != "" {
		if resolved, err := client.ResolveMessageID(ctx, action.RFCMessageID); err == nil && resolved != "" {
			msgID = resolved
		}
	}

	placement, folder := warmupFiling(action)

	for _, act := range action.Actions {
		switch act {
		case models.WarmupActionFile:
			// Out of Junk into the destination is one move, and on Exchange it
			// is also the "not junk" signal, so the rescue below finds nothing
			// left in Junk and correctly does nothing.
			var newID string
			var err error
			if placement == models.WarmupPlacementArchive {
				newID, err = client.MoveToArchive(ctx, msgID)
			} else {
				newID, err = client.MoveToFolder(ctx, msgID, folder)
			}
			if err != nil {
				log.Error().Err(err).Str("graph_id", msgID).Str("folder", folder).Msg("Failed to file warmup message (Graph)")
				continue
			}
			if newID != "" {
				w.remapProviderID(ctx, mail, msgID, newID)
				msgID = newID // subsequent actions target the moved copy
			}
		case models.WarmupActionMarkRead:
			if err := client.MarkAsRead(ctx, msgID); err != nil {
				log.Error().Err(err).Str("graph_id", msgID).Msg("Failed to mark as read (Graph)")
			}
		case models.WarmupActionRescueFromSpam:
			newID, err := client.RemoveFromSpam(ctx, msgID)
			if err != nil {
				log.Error().Err(err).Str("graph_id", msgID).Msg("Failed to rescue from junk (Graph)")
				continue
			}
			if newID != "" {
				w.remapProviderID(ctx, mail, msgID, newID)
				msgID = newID
			}
		case models.WarmupActionMarkImportant:
			if err := client.MarkImportant(ctx, msgID); err != nil {
				log.Error().Err(err).Str("graph_id", msgID).Msg("Failed to mark important (Graph)")
			}
		case models.WarmupActionStar:
			if err := client.AddFlag(ctx, msgID); err != nil {
				log.Error().Err(err).Str("graph_id", msgID).Msg("Failed to flag warmup message (Graph)")
			}
		case models.WarmupActionDelete:
			if msgID != "" {
				if err := client.Delete(ctx, msgID); err != nil {
					return fmt.Errorf("delete warmup message (Graph): %w", err)
				}
			}
			// The live id resolved above is in the map because every move
			// this worker made re-keyed it (remapProviderID), which is what
			// lets the sender's own copy, whose internal id the control
			// plane does not know, drop its body too.
			if err := w.dropWarmupBody(ctx, mail, action, msgID); err != nil {
				return err
			}
		default:
			log.Warn().Str("action", act).Msg("Unknown warmup action")
		}
	}
	return nil
}

// remapProviderID keeps the message map current across a Graph move, which
// is a copy plus delete and hands the message a new id. The map is keyed by
// the id the sync stored, so without this every message this worker filed is
// unreachable by its live id and the body it stored could never be dropped.
func (w *WorkerService) remapProviderID(ctx context.Context, mail *wmail.WMail, oldID, newID string) {
	if mail.EmailMessageMapRepository == nil || oldID == "" || newID == "" || oldID == newID {
		return
	}
	m, err := mail.EmailMessageMapRepository.Get(ctx, mail.UserID, mail.ID, oldID)
	if err != nil || m == nil {
		return
	}
	if err := mail.EmailMessageMapRepository.Add(ctx, repository.EmailMessageData{
		UserID: m.UserID, EmailID: m.EmailID, MessageID: newID, ID: m.ID, ThreadID: m.ThreadID,
	}); err != nil {
		log.Debug().Err(err).Str("email_id", mail.ID.String()).Msg("Could not re-key the moved message in the map")
	}
}

func (w *WorkerService) runImapWarmupActions(ctx context.Context, mail *wmail.WMail, action models.WarmupEmailAction) error {
	boxes := mail.SmtpImapData.Mailboxes
	imapClient := mail.SmtpImapData.ImapClient
	sourceBox := lookupWarmupSourceFolder(boxes, action)

	inboxName := "INBOX"
	if inboxBox := lookupInbox(boxes); inboxBox != nil {
		inboxName = inboxBox.Name
	}

	// dst is where the filing action puts this mailbox's warmup mail, and is
	// empty for the placement that leaves it where the provider put it.
	placement, folder := warmupFiling(action)
	dst := ""
	switch placement {
	case models.WarmupPlacementFolder:
		dst = folder
	case models.WarmupPlacementArchive:
		if box := lookupArchive(boxes); box != nil {
			dst = box.Name
		} else {
			// No archive on this server. The warmup folder is the destination
			// that always exists, because we create it.
			dst = folder
			log.Debug().Str("email_id", action.EmailID.String()).Msg("No archive folder on this account; filing warmup in its own folder")
		}
	}

	sentName := ""
	if sentBox := lookupSent(boxes); sentBox != nil {
		sentName = sentBox.Name
	}
	trashName := ""
	if trashBox := lookupTrash(boxes); trashBox != nil {
		trashName = trashBox.Name
	}

	files := hasWarmupAction(action.Actions, models.WarmupActionFile)
	deletes := hasWarmupAction(action.Actions, models.WarmupActionDelete)
	boxName, uid, searchErr := w.locateWarmupMessage(ctx, imapClient, action, sourceBox, files, dst, inboxName, sentName)
	if boxName == "" {
		if deletes && searchErr != nil {
			// A folder could not be searched, so absence is not established;
			// deleting the body now would leave the message with nothing to
			// key a retry by.
			return fmt.Errorf("locate warmup message for deletion: %w", searchErr)
		}
		log.Warn().
			Str("folder", action.MailboxFolder).
			Uint32("uid_validity", action.MailboxUIDValidity).
			Str("email_id", action.EmailID.String()).
			Msg("Warmup action skipped: the message could not be located in any folder")
		if deletes {
			// Already gone from the mailbox: an earlier delivery removed it
			// before the body drop failed, or the owner did. Either way the
			// body is the only thing left to do.
			return w.dropWarmupBody(ctx, mail, action, action.RFCMessageID)
		}
		return nil
	}

	// moved is set once the message leaves boxName. Every later action would
	// address a UID that folder no longer has, and IMAP silently ignores a
	// STORE on a UID that is gone, so they are skipped rather than aimed at
	// whatever message inherits the number.
	moved := false

	// A message still in Junk gets the not-junk keywords once, before either
	// move takes it out, because its UID is void afterwards.
	inJunk := sourceBox != nil && boxName == sourceBox.Name && imap.IsSpamMailbox(sourceBox.Name, sourceBox.Attrs)
	unjunked := false
	unjunk := func() {
		if !inJunk || unjunked {
			return
		}
		unjunked = true
		if err := imapClient.MarkNotJunk(ctx, boxName, uid); err != nil {
			log.Debug().Err(err).Uint32("uid", uid).Msg("Server refused not-junk keywords (IMAP)")
		}
	}

	for _, act := range action.Actions {
		if moved {
			continue
		}
		switch act {
		case models.WarmupActionFile:
			if dst == "" {
				continue
			}
			unjunk()
			// A message already in the destination is not moved, and its UID is
			// still good for the actions after this one.
			did, err := imapClient.MoveToFolder(ctx, boxName, dst, uid)
			if err != nil {
				log.Error().Err(err).Uint32("uid", uid).Str("folder", dst).Msg("Failed to file warmup message (IMAP)")
				continue
			}
			moved = did
		case models.WarmupActionMarkRead:
			if err := imapClient.MarkAsRead(ctx, boxName, uid); err != nil {
				log.Error().Err(err).Uint32("uid", uid).Msg("Failed to mark as read (IMAP)")
			}
		case models.WarmupActionRescueFromSpam:
			// Only a message still sitting in a Junk folder is rescued. With any
			// placement but "inbox" the filing move above already took it out of
			// Junk, which is itself the not-spam signal the server learns from.
			if !inJunk {
				continue
			}
			unjunk()
			if err := imapClient.RemoveFromSpam(ctx, boxName, inboxName, uid); err != nil {
				log.Error().Err(err).Uint32("uid", uid).Msg("Failed to remove from spam (IMAP)")
				continue
			}
			moved = true
		case models.WarmupActionMarkImportant:
			if err := imapClient.MarkImportant(ctx, boxName, uid); err != nil {
				log.Error().Err(err).Uint32("uid", uid).Msg("Failed to mark important (IMAP)")
			}
		case models.WarmupActionStar:
			// No-op on IMAP: \Flagged is already set by mark_important, so
			// starring here would just re-flag the same message. Star is a
			// Gmail-only distinct signal.
			continue
		case models.WarmupActionDelete:
			if err := imapClient.DeleteUID(ctx, boxName, trashName, uid); err != nil {
				return fmt.Errorf("delete warmup message (IMAP) uid %d in %q: %w", uid, boxName, err)
			}
			moved = true
			// The map is keyed by the RFC Message-ID on IMAP.
			if err := w.dropWarmupBody(ctx, mail, action, action.RFCMessageID); err != nil {
				return err
			}
		default:
			log.Warn().Str("action", act).Msg("Unknown warmup action")
		}
	}
	return nil
}

// dropWarmupBody removes the platform's stored copy of a warmup message's
// body once the message is gone from the mailbox. The body was written when
// the message was synced, before anything knew it was warmup, and nothing
// else ever comes back for it. The internal id keying the blob travels with
// the action when the control plane has it; otherwise it is looked up from
// the provider key the worker acted on. Finding neither leaves the blob to
// the mailbox's erasure, which sweeps the whole prefix. A store that refuses
// the delete is an error, so the bus offers the action again.
func (w *WorkerService) dropWarmupBody(ctx context.Context, mail *wmail.WMail, action models.WarmupEmailAction, providerKey string) error {
	if mail.Storage == nil {
		return nil
	}
	internalID, err := uuid.Parse(action.InternalID)
	if err != nil && providerKey != "" && mail.EmailMessageMapRepository != nil {
		if m, lerr := mail.EmailMessageMapRepository.Get(ctx, mail.UserID, mail.ID, providerKey); lerr == nil && m != nil {
			internalID, err = uuid.Parse(m.ID)
		}
	}
	if err != nil || internalID == uuid.Nil {
		log.Debug().Str("email_id", action.EmailID.String()).Msg("Warmup body left in place: no internal id to key it by")
		return nil
	}
	key := config.StorageEndpointEmailBody(mail.UserID, mail.ID, internalID)
	if err := mail.Storage.Delete(ctx, key); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("drop the stored warmup body: %w", err)
	}
	return nil
}

func lookupTrash(boxes []*models.Mailbox) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && imap.IsTrashMailbox(b.Name, b.Attrs) {
			return b
		}
	}
	return nil
}

// locateWarmupMessage resolves the folder and UID an action should act on.
//
// The arrival folder and UID travel with the action, and for the leg that files
// the message they are still correct. The delayed leg (read / important) is a
// separate event published after the filing already moved the message, so its
// UID addresses nothing in the folder the mail arrived in. IMAP answers a STORE
// on a missing UID with silence, which is why that leg had been doing nothing
// at all on every mailbox that files its warmup: the mail stayed unread, and an
// unread count on the folder is the thing its owner was not supposed to notice.
//
// The Message-ID is the one identifier a move does not change, so the likely
// destinations are searched for it, most likely first. An empty folder name
// means the message is nowhere we know to look; the error, when set with it,
// is the last search that failed, so the caller can tell "absent" from
// "could not look".
func (w *WorkerService) locateWarmupMessage(
	ctx context.Context,
	client warmupIMAPClient,
	action models.WarmupEmailAction,
	sourceBox *models.Mailbox,
	files bool,
	dst, inboxName, sentName string,
) (string, uint32, error) {
	if files && sourceBox != nil {
		return sourceBox.Name, action.UID, nil
	}
	var searchErr error
	if action.RFCMessageID != "" {
		// Ordered by likelihood: the destination a previous leg filed it into,
		// then the inbox a rescue put it back in, then Sent for our own copy of
		// a warmup send, then the folder the event says it arrived in.
		candidates := []string{dst, inboxName, sentName}
		if sourceBox != nil {
			candidates = append(candidates, sourceBox.Name)
		}
		seen := make(map[string]bool, len(candidates))
		for _, name := range candidates {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			uid, err := client.FindUIDByMessageID(ctx, name, action.RFCMessageID)
			if err != nil {
				log.Debug().Err(err).Str("folder", name).Str("email_id", action.EmailID.String()).Msg("Could not search a folder for the warmup message")
				searchErr = err
				continue
			}
			if uid != 0 {
				return name, uid, nil
			}
		}
	}
	if sourceBox != nil {
		return sourceBox.Name, action.UID, nil
	}
	return "", 0, searchErr
}

// warmupIMAPClient is the slice of the IMAP client the warmup actions use, so
// locateWarmupMessage can be exercised without a server.
type warmupIMAPClient interface {
	FindUIDByMessageID(ctx context.Context, mailboxName, rfcMessageID string) (uint32, error)
}

// hasWarmupAction reports whether the leg contains a given warmup action.
func hasWarmupAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

// lookupWarmupSourceFolder resolves the folder an action's UID lives in.
//
// The folder is found by name, its identity. The UIDVALIDITY still has to
// match: it is the generation the stored UID belongs to, and a server that
// reissued it has given that number to some other message, so acting on it
// would star or file a message nobody asked about. Nothing to act on is the
// right answer there.
//
// An action published before the folder name was carried has only the
// UIDVALIDITY to go on, which is the old behaviour and stays as the fallback.
func lookupWarmupSourceFolder(boxes []*models.Mailbox, action models.WarmupEmailAction) *models.Mailbox {
	if action.MailboxFolder == "" {
		return lookupMailboxByUIDValidity(boxes, action.MailboxUIDValidity)
	}
	for _, b := range boxes {
		if b != nil && b.Name == action.MailboxFolder && b.UIDValidity == action.MailboxUIDValidity {
			return b
		}
	}
	return nil
}

func lookupMailboxByUIDValidity(boxes []*models.Mailbox, uidValidity uint32) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && b.UIDValidity == uidValidity {
			return b
		}
	}
	return nil
}

func lookupInbox(boxes []*models.Mailbox) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && imap.IsInboxMailbox(b.Name, b.Attrs) {
			return b
		}
	}
	return nil
}

func lookupSent(boxes []*models.Mailbox) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && imap.IsSentMailbox(b.Name, b.Attrs) {
			return b
		}
	}
	return nil
}

func lookupArchive(boxes []*models.Mailbox) *models.Mailbox {
	for _, b := range boxes {
		if b != nil && imap.IsArchiveMailbox(b.Name, b.Attrs) {
			return b
		}
	}
	return nil
}

// warmupFiling resolves where the filing action should put the message, from
// what the control plane sent with the action. An event published before these
// fields existed carries neither, and the default folder is exactly what every
// mailbox did then, so an in-flight event behaves the same either way.
func warmupFiling(action models.WarmupEmailAction) (placement, folder string) {
	placement = action.Placement
	if !models.ValidWarmupPlacement(placement) {
		placement = models.WarmupPlacementFolder
	}
	folder = strings.TrimSpace(action.TargetFolder)
	if folder == "" {
		folder = config.WarmupFolderDefault
	}
	return placement, folder
}
