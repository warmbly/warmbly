package tasks

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GetCampaignSequences returns the sequences for a campaign ordered by position
func (s *tasksService) GetCampaignSequences(ctx context.Context, campaignID uuid.UUID) ([]models.Sequence, error) {
	return s.campaignRepo.GetSequencesByCampaignID(ctx, campaignID)
}

// testContact stands in when no real contact is chosen for a test send.
func testContact(recipient string) models.Contact {
	return models.Contact{
		ID:        uuid.New(),
		FirstName: "Test",
		LastName:  "Recipient",
		Email:     recipient,
		Company:   "Test Company",
	}
}

// SendTestEmail renders a campaign step as the send path would and mails it to
// recipient through one of the organization's mailboxes. contact, when given,
// is the real contact the copy is rendered for; nil uses a placeholder. The
// message carries the campaign's attachments, the mailbox signature and the
// opt-out footer, so what lands in the tester's inbox is what a lead gets.
func (s *tasksService) SendTestEmail(ctx context.Context, orgID uuid.UUID, accountID uuid.UUID, recipient string, campaign *models.Campaign, sequence *models.Sequence, contact *models.Contact) *errx.Error {
	// Any member allowed to send may test from any of the organization's
	// mailboxes, not only the ones they connected. GetByID is the full row
	// (the org-scoped Get omits worker_id, which the send needs).
	account, err := s.emailRepo.GetByID(ctx, accountID)
	if err != nil || account == nil || account.OrganizationID == nil || *account.OrganizationID != orgID {
		return errx.New(errx.NotFound, "email account not found")
	}

	renderFor := testContact(recipient)
	if contact != nil {
		renderFor = *contact
	}

	// A test send carries the real opt-out footer and header so the sender
	// sees exactly what a recipient will, but its link names no contact
	// (uuid.Nil), so clicking it can never suppress anyone.
	optOut := s.resolveOptOut(ctx, orgID, campaign)
	var unsubscribeURL string
	if s.unsubLinks != nil && s.unsubLinks.Enabled() {
		unsubscribeURL = s.mintUnsubscribeLink(ctx, resolveOptOutOrigin(account, campaign), orgID, campaign.ID, uuid.Nil)
	}

	rendered := previewTemplatesWith(sequence.Subject, sequence.BodyHTML, sequence.BodyPlain, renderFor, unsubscribeURL)
	bodyHTML, bodyPlain := finishBody(rendered.BodyHTML, rendered.BodyPlain, campaign.TextOnly, account, &optOut, unsubscribeURL)
	subject := "[TEST] " + rendered.Subject

	headerURL := ""
	if campaign.UnsubscribeHeader {
		headerURL = unsubscribeURL
	}

	// Tracking is deliberately off: a test open or click must not count.
	emailMsg := EmailMessage{
		From:           account.Email,
		To:             []string{recipient},
		Subject:        subject,
		BodyHTML:       bodyHTML,
		BodyPlain:      bodyPlain,
		MessageID:      generateMessageID(account.SendFrom()),
		IsWarmup:       false,
		UnsubscribeURL: headerURL,
		Attachments:    s.campaignAttachmentRefs(ctx, campaign.ID, sequence.ID),
	}

	taskID := uuid.New()
	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		return errx.New(errx.Internal, fmt.Sprintf("failed to send test email: %v", err))
	}

	return nil
}
