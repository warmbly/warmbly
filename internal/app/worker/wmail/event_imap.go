package wmail

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

func (w *WMail) onImapEmailUpdate(ctx context.Context, msg *models.EmailMessageData) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, msg.MessageID)
	if err != nil {
		return err
	}

	if internalMessage == nil {
		// Check and record sync event for rate limiting (new email)
		if rateLimitErr := w.CheckAndRecordSync(ctx, 1); rateLimitErr != nil {
			return rateLimitErr
		}

		msg.ID = uuid.New()
		now := time.Now()

		var threadID string
		var parentID string
		if len(msg.InReplyTo) > 0 {
			parentID = msg.InReplyTo[len(msg.InReplyTo)-1]
		} else if len(msg.ReplyTo) > 0 {
			parentID = msg.ReplyTo[len(msg.ReplyTo)-1]
		}

		if parentID != "" {
			internalParent, _ := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, parentID)
			if internalParent != nil {
				threadID = internalParent.ID
			}
		} else {
			threadID = msg.MessageID
		}

		snippet := GenerateSnippet(msg.BodyPlain, msg.BodyHTML)

		data := &models.EmailMessageStoreData{
			ID:           msg.ID,
			EmailID:      w.ID,
			Mailbox:      w.SmtpImapData.mailbox,
			ThreadID:     threadID,
			MessageID:    msg.MessageID,
			GmailID:      msg.GmailID,
			ParentID:     parentID,
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
			Snippet:      snippet,
			Seen:         false,
			UpdatedAt:    now,
			CreatedAt:    now,
		}

		if err := w.EmailMessageMapRepository.Add(ctx, repository.EmailMessageData{
			UserID:    w.UserID.String(),
			EmailID:   w.UserID.String(),
			MessageID: data.MessageID,
			ID:        w.ID.String(),
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
	} else {
		internalID, err := uuid.Parse(internalMessage.ID)
		if err != nil {
			return err
		}

		if err := w.onEvent(models.JobEventTypeEmailUpdate, &models.JobEventEmailUpdate{
			UserID:  w.UserID,
			EmailID: w.ID,
			ID:      internalID,
			UID:     msg.UID,
			ModSeq:  msg.ModSeq,
			Mailbox: w.SmtpImapData.mailbox,
			Flags:   msg.Flags,
		}); err != nil {
			return err
		}
	}

	return nil
}
