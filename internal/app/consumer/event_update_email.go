package jobs

import (
	"context"
	"fmt"
	"slices"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) HandleUpdateEmail(ctx context.Context, e *models.JobEventEmailUpdate) error {
	email, err := s.UniboxRepository.GetByID(ctx, e.UserID, e.ID)
	if err != nil {
		CaptureError(e.UserID, e.EmailID, fmt.Errorf("Email (%s): %w", e.ID.String(), err))
		return err
	}

	var updateData repository.UpdateUniboxEntry

	if !slices.Equal(email.Flags, e.Flags) {
		updateData.Flags = e.Flags
	}
	if email.UID != e.UID {
		updateData.UID = &e.UID
	}
	if email.Mailbox != e.Mailbox {
		updateData.Mailbox = &e.Mailbox
	}
	if email.ModSeq != e.ModSeq {
		updateData.ModSeq = &e.ModSeq
	}

	if err := s.UniboxRepository.UpdateEntry(ctx, e.UserID, e.EmailID, e.ID, &updateData); err != nil {
		return err
	}

	email.Flags = e.Flags
	email.UID = e.UID
	email.Mailbox = e.Mailbox
	email.ModSeq = e.ModSeq
	s.publishEmailUpdated(ctx, e.UserID, email)
	return nil
}
