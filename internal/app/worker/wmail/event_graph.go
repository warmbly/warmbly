package wmail

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

// onGraphMessageAdd mirrors onGoogleMessageAdd: dedupe by RFC Message-ID, store
// the body, record the messageId map entry, and emit a NEW_EMAIL event. The
// opaque Graph message id rides in GmailID (the provider-message-id field).
func (w *WMail) onGraphMessageAdd(ctx context.Context, msg *models.EmailMessageData) error {
	// The messageId map is keyed by the provider message id (the Graph id lives
	// in GmailID), so dedup and the remove/flag lookups below all use the same
	// key even though delta only ever gives us the Graph id.
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, msg.GmailID)
	if err != nil {
		return err
	}

	if internalMessage != nil {
		return nil
	}

	if rateLimitErr := w.CheckAndRecordSync(ctx, 1); rateLimitErr != nil {
		return rateLimitErr
	}

	msg.ID = uuid.New()
	now := time.Now()

	data := &models.EmailMessageStoreData{
		ID:           msg.ID,
		EmailID:      w.ID,
		Mailbox:      0,
		ThreadID:     msg.ThreadID,
		MessageID:    msg.MessageID,
		GmailID:      msg.GmailID,
		ParentID:     msg.ParentID,
		UID:          msg.UID,
		ModSeq:       msg.ModSeq,
		Flags:        msg.Flags,
		BCC:          msg.BCC,
		CC:           msg.CC,
		FromAddr:     msg.From,
		InReplyTo:    msg.InReplyTo,
		ReplyTo:      msg.ReplyTo,
		ToAddr:       msg.To,
		Subject:      msg.Subject,
		Size:         msg.Size,
		InternalDate: msg.InternalDate,
		SentDate:     msg.Date,
		Snippet:      msg.Snippet,
		Seen:         false,
		UpdatedAt:    now,
		CreatedAt:    now,
	}

	if err := w.EmailMessageMapRepository.Add(ctx, repository.EmailMessageData{
		UserID:    w.UserID.String(),
		EmailID:   w.ID.String(),
		MessageID: msg.GmailID,
		ID:        msg.ID.String(),
		ThreadID:  msg.ThreadID,
	}); err != nil {
		return err
	}

	if err := w.StoreBody(ctx, msg.ID, &emsg.EmailBlob{
		HTMLBody:  []byte(msg.BodyHTML),
		PlainText: []byte(msg.BodyPlain),
	}); err != nil {
		return err
	}

	w.maybeEmitBounce(msg)
	return w.onEvent(models.JobEventTypeNewEmail, data)
}

// onGraphMessageRemove emits REMOVE_EMAIL for a message deleted or moved out of a
// tracked folder. providerID is the Graph message id.
func (w *WMail) onGraphMessageRemove(ctx context.Context, providerID string) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, providerID)
	if err != nil {
		return err
	}
	if internalMessage == nil {
		return nil
	}

	internalID, err := uuid.Parse(internalMessage.ID)
	if err != nil {
		return err
	}

	return w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      internalID,
	})
}

// onGraphFlagsChange keeps read state in sync: Graph delta reports isRead, which
// we map to the \Seen flag add/remove the unibox already understands. No-op when
// the message isn't tracked yet (the add path sets the initial flags).
func (w *WMail) onGraphFlagsChange(ctx context.Context, providerID string, seen bool) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, providerID)
	if err != nil {
		return err
	}
	if internalMessage == nil {
		return nil
	}

	internalID, err := uuid.Parse(internalMessage.ID)
	if err != nil {
		return err
	}

	eventType := models.JobEventTypeFlagsRemove
	if seen {
		eventType = models.JobEventTypeFlagsAdd
	}

	return w.onEvent(eventType, &models.JobEventFlags{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      internalID,
		Flags:   []string{"\\Seen"},
	})
}

// onGraphDelta relays the opaque per-folder delta cursor to the control plane for
// durable persistence (the worker is disposable and must not be the source of
// truth for the cursor).
func (w *WMail) onGraphDelta(_ context.Context, folder, deltaLink string) error {
	return w.onEvent(models.JobEventTypeGraphDeltaUpdate, &models.JobEventGraphDeltaUpdate{
		UserID:    w.UserID,
		EmailID:   w.ID,
		Folder:    folder,
		DeltaLink: deltaLink,
	})
}

// cloneStringMap returns a shallow copy so the worker's live cursor map is never
// aliased to the deserialized event payload.
func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
