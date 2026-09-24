package unibox

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/repository"
)

// ForwardSource reads a message for a forward. Org-scoped like GetByID, but
// it leaves the read state alone: forwarding is not reading.
func (s *uniboxService) ForwardSource(ctx context.Context, orgID, id uuid.UUID) (*models.ForwardedMessage, *errx.Error) {
	msg, ownerID, err := s.uniboxRepository.GetByIDForOrg(ctx, orgID, id)
	if err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, errx.New(errx.NotFound, "message to forward not found")
		}
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	out := &models.ForwardedMessage{
		EmailID: msg.EmailID,
		From:    msg.FromAddr,
		To:      msg.ToAddr,
		CC:      msg.CC,
		Subject: msg.Subject,
		Date:    msg.SentDate,
	}
	if out.Date.IsZero() {
		out.Date = msg.InternalDate
	}

	body, err := s.GetBody(ctx, ownerID, msg.EmailID, id)
	if err != nil {
		// Same degraded read as GetByID: the preview is what is stored.
		if !isFixtureMessage(msg.MessageID) {
			errs.CaptureException(err)
		}
		out.BodyPlain = msg.Snippet
		out.Truncated = !isFixtureMessage(msg.MessageID)
		return out, nil
	}

	out.BodyPlain = string(body.PlainText)
	out.BodyHTML = string(body.HTMLBody)
	if out.BodyHTML != "" && !mailhtml.LooksLikeHTML(out.BodyHTML) {
		// Legacy rows stored the plain text under both bodies.
		if out.BodyPlain == "" {
			out.BodyPlain = out.BodyHTML
		}
		out.BodyHTML = ""
	}
	return out, nil
}
