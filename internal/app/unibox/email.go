package unibox

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *uniboxService) GetByID(
	ctx context.Context,
	orgID, id uuid.UUID,
) (*models.EmailMessage, *errx.Error) {
	var resp models.EmailMessage
	var snippet string
	var seededMessage bool

	// ownerID is the mailbox owner's user_id. The S3 body key is built from it
	// (emails/<ownerID>/<id>), so the body must be fetched under the owner even
	// when a different teammate opens the message via the org-scoped read.
	var ownerID uuid.UUID

	// Fetch email data by id index
	{
		msg, owner, err := s.uniboxRepository.GetByIDForOrg(ctx, orgID, id)
		if err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
		ownerID = owner
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
		snippet = msg.Snippet
		seededMessage = strings.HasPrefix(msg.MessageID, "<seed-")
	}

	// Fetch body from s3 storage. Keyed by the mailbox OWNER's user_id, not the
	// caller's: the key is emails/<ownerID>/<id>.
	{
		out, err := s.GetBody(ctx, ownerID, id)
		if err != nil {
			sentry.CaptureException(err)
			if seededMessage {
				resp.BodyPlain = snippet
				return &resp, nil
			}
			return nil, errx.InternalError()
		}

		resp.BodyPlain = string(out.PlainText)
		resp.BodyHTML = string(out.HTMLBody)
	}

	return &resp, nil
}
