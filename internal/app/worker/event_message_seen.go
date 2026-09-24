package worker

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/models"
)

// HandleMessageSeen applies a read/unread change made in the unibox to the
// mailbox itself, so the provider's copy agrees with what the customer sees.
//
// Best-effort by design: the store is already updated when this arrives, and a
// message the provider has since moved or deleted must not turn into a failed
// job. Every provider path logs and moves on, and the next sync brings back
// whatever the provider actually thinks.
func (w *WorkerService) HandleMessageSeen(ctx context.Context, action models.MessageSeenAction) error {
	if len(action.Messages) == 0 {
		return nil
	}

	mail, exists := w.loadedMailbox(ctx, action.EmailID)
	if !exists {
		// The mailbox is not loaded here (a restart, or it moved worker mid
		// flight). The read state is already correct in Warmbly; only the
		// provider's copy misses out.
		log.Warn().Str("email_id", action.EmailID.String()).Msg("Mailbox not loaded; read state not relayed to the provider")
		return nil
	}

	log.Info().
		Str("email_id", action.EmailID.String()).
		Bool("seen", action.Seen).
		Int("messages", len(action.Messages)).
		Msg("Relaying unibox read state to the provider")

	switch {
	case mail.GoogleData != nil && mail.GoogleData.Client != nil:
		w.relaySeenGoogle(ctx, mail, action)
	case mail.GraphData != nil && mail.GraphData.Client != nil:
		w.relaySeenGraph(ctx, mail, action)
	case mail.SmtpImapData != nil && mail.SmtpImapData.ImapClient != nil:
		w.relaySeenImap(ctx, mail, action)
	default:
		log.Warn().Str("email_id", action.EmailID.String()).Msg("No mail client available to relay read state")
	}
	return nil
}

// relaySeenGoogle sends the whole batch as one batchModify.
func (w *WorkerService) relaySeenGoogle(ctx context.Context, mail *wmail.WMail, action models.MessageSeenAction) {
	ids := make([]string, 0, len(action.Messages))
	for _, m := range action.Messages {
		if m.ProviderID != "" {
			ids = append(ids, m.ProviderID)
		}
	}
	if len(ids) == 0 {
		return
	}
	if err := mail.GoogleData.Client.SetSeen(ctx, ids, action.Seen); err != nil {
		log.Warn().Err(err).Str("email_id", action.EmailID.String()).Int("messages", len(ids)).
			Msg("Failed to relay read state to Gmail")
	}
}

// relaySeenGraph patches one message at a time, re-resolving ids that moved.
func (w *WorkerService) relaySeenGraph(ctx context.Context, mail *wmail.WMail, action models.MessageSeenAction) {
	client := mail.GraphData.Client
	for _, m := range action.Messages {
		id := m.ProviderID
		// A Graph message id changes when the message is moved (copy+delete),
		// so prefer the immutable Message-ID when one travelled.
		if m.RFCMessageID != "" {
			if resolved, err := client.ResolveMessageID(ctx, m.RFCMessageID); err == nil && resolved != "" {
				id = resolved
			}
		}
		if id == "" {
			continue
		}
		if err := client.SetSeen(ctx, id, action.Seen); err != nil {
			log.Warn().Err(err).Str("email_id", action.EmailID.String()).Str("graph_id", id).
				Msg("Failed to relay read state to Outlook")
		}
	}
}

// relaySeenImap groups by folder, because a UID only means anything inside
// one and each folder costs a SELECT.
func (w *WorkerService) relaySeenImap(ctx context.Context, mail *wmail.WMail, action models.MessageSeenAction) {
	byFolder := make(map[string][]uint32)
	for _, m := range action.Messages {
		if m.UID == 0 || m.Folder == "" {
			continue
		}
		byFolder[m.Folder] = append(byFolder[m.Folder], m.UID)
	}
	for folder, uids := range byFolder {
		if err := mail.SmtpImapData.ImapClient.SetSeen(ctx, folder, uids, action.Seen); err != nil {
			log.Warn().Err(err).Str("email_id", action.EmailID.String()).Str("folder", folder).Int("uids", len(uids)).
				Msg("Failed to relay read state over IMAP")
		}
	}
}
