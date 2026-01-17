package unibox

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *uniboxService) GetByID(
	ctx context.Context,
	userID, id uuid.UUID,
) (*models.EmailMessage, *errx.Error) {
	var resp models.EmailMessage

	// Fetch email data by id index
	{
		msg, err := s.uniboxRepository.GetByID(ctx, userID, id)
		if err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
		resp.ID = msg.ID
		resp.GmailID = msg.GmailID
		resp.UID = msg.UID

		resp.ParentID = msg.ParentID
		resp.ThreadID = msg.ThreadID

		resp.Flags = msg.Flags

		resp.BCC = msg.BCC
		resp.CC = msg.CC
		resp.Date = msg.SentDate
		resp.From = msg.FromAddr
		resp.InReplyTo = msg.InReplyTo
		resp.MessageID = msg.MessageID
		resp.ReplyTo = msg.ReplyTo
		resp.To = msg.ToAddr
		resp.Subject = msg.Subject

		resp.Size = msg.Size
		resp.InternalDate = msg.InternalDate
		resp.ModSeq = msg.ModSeq
	}

	// Fetch body from s3 storage
	{
		out, err := s.GetBody(ctx, userID, id)
		if err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}

		resp.BodyPlain = string(out.PlainText)
		resp.BodyHTML = string(out.HTMLBody)
	}

	return &resp, nil
}
