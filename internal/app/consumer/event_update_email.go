package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) HandleUpdateEmail(ctx context.Context, e *models.JobEventEmailUpdate) error {
	email, err := s.emailForSyncUpdate(ctx, e.UserID, e.ID, func(message *models.EmailMessageStoreData) {
		token := warmupTokenFromMessage(message)
		message.Flags = append([]string{}, e.Flags...)
		if token != "" {
			message.Flags = append(message.Flags, config.WarmupVerifyHeader+":"+token)
		}
		message.UID, message.Mailbox, message.ModSeq = e.UID, e.Mailbox, e.ModSeq
		if e.FolderPath != "" {
			message.FolderPath = e.FolderPath
		}
		if e.Folder != "" {
			message.Folder = models.NormalizeFolder(e.Folder, e.Flags)
		}
	})
	if err != nil {
		CaptureError(e.UserID, e.EmailID, fmt.Errorf("Email (%s): %w", e.ID.String(), err))
		return err
	}
	if email == nil {
		return nil
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
	// The source folder's name, which is its identity. Empty on events from
	// workers predating the field, which keeps the stored value.
	if e.FolderPath != "" && email.FolderPath != e.FolderPath {
		updateData.FolderPath = &e.FolderPath
	}
	// The store follows the provider only when the provider itself moved the
	// message; a scan that keeps naming the same folder leaves local filing be.
	folder, provider, providerMoved := models.ResolveFolderSync(email.Folder, email.ProviderFolder, e.Folder)
	if providerMoved {
		updateData.ProviderFolder = &provider
		if folder != email.Folder {
			updateData.Folder = &folder
		}
	}

	if err := s.UniboxRepository.UpdateEntry(ctx, e.UserID, e.EmailID, e.ID, &updateData); err != nil {
		return err
	}

	email.Flags = e.Flags
	email.UID = e.UID
	email.Mailbox = e.Mailbox
	email.ModSeq = e.ModSeq
	if providerMoved {
		email.Folder = folder
		email.ProviderFolder = provider
	}
	s.publishEmailUpdated(ctx, e.UserID, email)
	return nil
}

// emailForSyncUpdate rechecks visible mail if verification won the pending-row lock.
func (s *JobsService) emailForSyncUpdate(ctx context.Context, userID, id uuid.UUID, updatePending func(*models.EmailMessageStoreData)) (*models.EmailMessageStoreData, error) {
	message, err := s.UniboxRepository.GetByID(ctx, userID, id)
	if !errors.Is(err, repository.ErrEmailNotFound) {
		return message, err
	}
	updated, err := s.UniboxRepository.UpdatePendingEmail(ctx, userID, id, updatePending)
	if err != nil || updated {
		return nil, err
	}
	message, err = s.UniboxRepository.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrEmailNotFound) {
		return nil, nil
	}
	return message, err
}
