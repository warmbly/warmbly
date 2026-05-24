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

// SendTestEmail renders a campaign email and sends it to a test recipient for preview
func (s *tasksService) SendTestEmail(ctx context.Context, userID string, accountID uuid.UUID, recipient string, campaign *models.Campaign, sequence *models.Sequence) *errx.Error {
	// Load the email account
	account, err := s.emailRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return errx.New(errx.NotFound, "email account not found")
	}

	// Verify account belongs to user
	if account.UserID != userID {
		return errx.ErrForbidden
	}

	// Create a dummy contact for template rendering
	testContact := models.Contact{
		ID:        uuid.New(),
		FirstName: "Test",
		LastName:  "Recipient",
		Email:     recipient,
		Company:   "Test Company",
	}

	// Render templates with the test contact
	subject := RenderTemplate(sequence.Subject, testContact)
	bodyHTML := RenderTemplate(sequence.BodyHTML, testContact)
	bodyPlain := RenderTemplate(sequence.BodyPlain, testContact)

	if bodyPlain == "" && bodyHTML != "" {
		bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
	}

	// Prepend [TEST] to subject
	subject = "[TEST] " + subject

	// Add signature if enabled
	if account.SignatureSync {
		bodyHTML = AddSignature(bodyHTML, account.SignatureHTML, true)
		bodyPlain = AddSignature(bodyPlain, account.SignaturePlain, false)
	}

	// Generate message ID
	messageID := generateMessageID(account.Email)

	// Build tracking info (disabled for test emails)
	emailMsg := EmailMessage{
		From:      account.Email,
		To:        []string{recipient},
		Subject:   subject,
		BodyHTML:  bodyHTML,
		BodyPlain: bodyPlain,
		MessageID: messageID,
		IsWarmup:  false,
		UserID:    uuid.MustParse(userID),
	}

	taskID := uuid.New()
	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		return errx.New(errx.Internal, fmt.Sprintf("failed to send test email: %v", err))
	}

	return nil
}
