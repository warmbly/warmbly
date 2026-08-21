package unibox

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

func (s *uniboxService) GetByID(
	ctx context.Context,
	orgID, id uuid.UUID,
) (*models.EmailMessage, *errx.Error) {
	var resp models.EmailMessage
	var snippet string
	var fixtureMessage bool

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
		fixtureMessage = isFixtureMessage(msg.MessageID)
	}

	// Fetch body from s3 storage. Keyed by the mailbox OWNER's user_id, not the
	// caller's: the key is emails/<ownerID>/<id>.
	{
		out, err := s.GetBody(ctx, ownerID, id)
		if err != nil {
			// A missing blob is a degraded read, not a broken endpoint: mail
			// synced before body storage existed, or a blob that never landed,
			// still has its preview text. Returning 500 made the whole message
			// unopenable instead of showing what we have.
			if !fixtureMessage {
				sentry.CaptureException(err)
			}
			resp.BodyPlain = snippet
			resp.BodyTruncated = !fixtureMessage
			return &resp, nil
		}

		resp.BodyPlain = string(out.PlainText)

		htmlBody := string(out.HTMLBody)
		if htmlBody != "" && !mailhtml.LooksLikeHTML(htmlBody) {
			// Legacy rows: a sync from before the IMAP reader addressed parts
			// individually stored the plain text under both bodies. Serving it
			// as HTML is what collapsed the message to one line.
			if resp.BodyPlain == "" {
				resp.BodyPlain = htmlBody
			}
			htmlBody = ""
		}
		// The HTML body comes from the sender's mail client and is rendered in
		// the dashboard, so it is sanitized here rather than at each call site.
		resp.BodyHTML = mailhtml.Sanitize(htmlBody)
	}

	return &resp, nil
}

// isFixtureMessage reports whether a message came from the seed/sandbox
// fixtures rather than a real mailbox sync. Fixture rows carry their whole
// content in the snippet and have no body blob, so a missing body is expected
// for them and must not be flagged to the reader as a truncated message.
func isFixtureMessage(messageID string) bool {
	for _, prefix := range []string{"<seed-", "<sbx-", "<dev-"} {
		if strings.HasPrefix(messageID, prefix) {
			return true
		}
	}
	return false
}
