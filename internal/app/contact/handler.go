package contact

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *contactService) Add(ctx context.Context, userID string, contacts []models.AddContact) ([]models.Contact, *errx.Error) {
	// Enforce contact limit if subscription repos are available
	if s.subRepo != nil && s.planRepo != nil {
		uid, parseErr := uuid.Parse(userID)
		if parseErr == nil {
			sub, err := s.subRepo.GetByUserID(ctx, uid)
			if err == nil && sub != nil {
				plan, err := s.planRepo.GetByID(ctx, sub.PlanID)
				if err == nil && plan != nil && plan.MaxContacts > 0 {
					currentCount, xerr := s.contactRepository.GetContactCount(ctx, userID)
					if xerr == nil {
						newTotal := currentCount + len(contacts)
						if newTotal > int(plan.MaxContacts) {
							return nil, errx.New(errx.Forbidden, "contact limit reached for your plan")
						}
					}
				}
			}
		}
	}

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
