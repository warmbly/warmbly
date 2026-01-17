package contact

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *contactService) Add(ctx context.Context, userID string, contacts []models.AddContact) ([]models.Contact, *errx.Error) {
	return s.contactRepository.Add(ctx, userID, contacts)
}

func (s *contactService) Search(ctx context.Context, userID, cursor, category, limit string, filters models.SearchContacts) (*models.ContactsResult, *errx.Error) {
	cursorId, err := validate.Uuid(cursor)
	if err != nil {
		return nil, err
	}
	categoryId, err := validate.Uuid(category)
	if err != nil {
		return nil, err
	}

	limitN, err := validate.Limit(limit)
	if err != nil {
		return nil, err
	}

	return s.contactRepository.Search(ctx, userID, categoryId, cursorId, filters, limitN)
}

func (s *contactService) BulkUpdate(ctx context.Context, userID string, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error) {
	return s.contactRepository.BulkUpdate(ctx, userID, data)
}

func (s *contactService) Update(ctx context.Context, userID, contactID string, data *models.UpdateContact) (*models.Contact, *errx.Error) {
	return s.contactRepository.Update(ctx, userID, contactID, data)
}

func (s *contactService) BulkDelete(ctx context.Context, userID string, contactIDs []string) *errx.Error {
	return s.contactRepository.BulkDelete(ctx, userID, contactIDs)
}

func (s *contactService) Delete(ctx context.Context, userID string, contactID string) *errx.Error {
	return s.contactRepository.Delete(ctx, userID, contactID)
}
