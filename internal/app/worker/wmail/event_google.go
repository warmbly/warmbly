package wmail

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

func (w *WMail) onGoogleMessageAdd(ctx context.Context, msg *models.EmailMessageData) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, msg.MessageID)
	if err != nil {
		return err
	}

	if internalMessage != nil {
		return nil
	}

	// Check and record sync event for rate limiting
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
		MessageID: data.MessageID,
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

	if err := w.onEvent(models.JobEventTypeNewEmail, data); err != nil {
		return err
	}

	return nil
}

func (w *WMail) onGoogleMessageRemove(ctx context.Context, messageID string) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, messageID)
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

	if err := w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      internalID,
	}); err != nil {
		return err
	}

	return nil
}

func (w *WMail) onGoogleMessageLabelsAdded(ctx context.Context, messageID string, labelIDs []string) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, messageID)
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

	if err := w.onEvent(models.JobEventTypeFlagsAdd, &models.JobEventFlags{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      internalID,
		Flags:   labelIDs,
	}); err != nil {
		return err
	}

	return nil
}

func (w *WMail) onGoogleMessageLabelsRemoved(ctx context.Context, messageID string, labelIDs []string) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, messageID)
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

	if err := w.onEvent(models.JobEventTypeFlagsRemove, &models.JobEventFlags{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      internalID,
		Flags:   labelIDs,
	}); err != nil {
		return err
	}

	return nil
}
