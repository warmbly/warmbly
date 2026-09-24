package unibox

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *uniboxService) GetByID(
	ctx context.Context,
	orgID, id uuid.UUID,
) (*models.EmailMessage, *errx.Error) {
	var resp models.EmailMessage
	var snippet string
	var fixtureMessage bool

	// The body's object-storage key is built from the mailbox owner and
	// account, not the caller, who may be any teammate on the org-scoped read.
	var ownerID, accountID uuid.UUID

	// Fetch email data by id index
	{
		msg, owner, err := s.uniboxRepository.GetByIDForOrg(ctx, orgID, id)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, errx.New(errx.NotFound, "message not found")
			}
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		ownerID = owner
		accountID = msg.EmailID

		// Opening a conversation is what marks it read, org-scoped so any
		// member clears the shared unread state. Relayed like any other read
		// state change, so the mailbox agrees: this is the path a developer
		// hitting the API reaches, where nothing calls PATCH /unibox/seen.
		if !msg.Seen {
			if changed, err := s.uniboxRepository.MarkSeenBulk(ctx, orgID, []uuid.UUID{id}, true); err == nil {
				msg.Seen = true
				s.relaySeen(ctx, orgID, changed)
			}
		}
		resp.ID = msg.ID
		resp.GmailID = msg.GmailID
		resp.UID = msg.UID
		resp.EmailID = msg.EmailID
		resp.Folder = models.NormalizeFolder(msg.Folder, msg.Flags)

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

	// Fetch body from object storage under the mailbox owner and account.
	var htmlBody string
	resp.BodyPlain, htmlBody, resp.BodyTruncated = s.storedBody(ctx, ownerID, accountID, id, snippet, fixtureMessage)
	// The HTML body comes from the sender's mail client and is rendered in
	// the dashboard, so it is sanitized here rather than at each call site.
	resp.BodyHTML = mailhtml.Sanitize(htmlBody)

	return &resp, nil
}

// storedBody reads a message's stored text and raw HTML. A missing blob is a
// degraded read, not a failure: mail synced before body storage existed, or a
// blob that never landed, still has its preview, returned as the text and
// flagged truncated.
func (s *uniboxService) storedBody(ctx context.Context, ownerID, accountID, id uuid.UUID, snippet string, fixture bool) (plain, rawHTML string, truncated bool) {
	out, err := s.GetBody(ctx, ownerID, accountID, id)
	if err != nil {
		if !fixture {
			errs.CaptureException(err)
		}
		return snippet, "", !fixture
	}
	plain, rawHTML = string(out.PlainText), string(out.HTMLBody)
	if rawHTML != "" && !mailhtml.LooksLikeHTML(rawHTML) {
		// Legacy rows: a sync from before the IMAP reader addressed parts
		// individually stored the plain text under both bodies. Serving it
		// as HTML is what collapsed the message to one line.
		if plain == "" {
			plain = rawHTML
		}
		rawHTML = ""
	}
	return plain, rawHTML, false
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
