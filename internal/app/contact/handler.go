package contact

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *contactService) Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error) {
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

	created, xerr := s.contactRepository.Add(ctx, userID, orgID, contacts)
	if xerr != nil {
		return nil, xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:add")
	return created, nil
}

func (s *contactService) Search(ctx context.Context, orgID, cursor, category, limit string, filters models.SearchContacts) (*models.ContactsResult, *errx.Error) {
	cursorId, err := paging.DecodeCursor(cursor)
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

	return s.contactRepository.Search(ctx, orgID, categoryId, cursorId, filters, limitN)
}

func (s *contactService) BulkUpdate(ctx context.Context, userID string, orgID uuid.UUID, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error) {
	updated, xerr := s.contactRepository.BulkUpdate(ctx, userID, orgID, data)
	if xerr != nil {
		return nil, xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:bulk_update")
	return updated, nil
}

func (s *contactService) Update(ctx context.Context, userID, contactID string, orgID uuid.UUID, data *models.UpdateContact) (*models.Contact, *errx.Error) {
	updated, xerr := s.contactRepository.Update(ctx, userID, contactID, orgID, data)
	if xerr != nil {
		return nil, xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:update:"+contactID)
	return updated, nil
}

func (s *contactService) BulkDelete(ctx context.Context, userID string, orgID uuid.UUID, contactIDs []string) *errx.Error {
	if xerr := s.contactRepository.BulkDelete(ctx, userID, orgID, contactIDs); xerr != nil {
		return xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:bulk_delete")
	return nil
}

func (s *contactService) Delete(ctx context.Context, userID string, orgID uuid.UUID, contactID string) *errx.Error {
	if xerr := s.contactRepository.Delete(ctx, userID, orgID, contactID); xerr != nil {
		return xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:delete:"+contactID)
	return nil
}

func (s *contactService) GetDetail(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID) (*models.ContactDetail, *errx.Error) {
	return s.contactRepository.GetDetail(ctx, userID, orgID, contactID)
}

func (s *contactService) GetByEmail(ctx context.Context, orgID *uuid.UUID, email string) (*models.Contact, *errx.Error) {
	if orgID == nil || strings.TrimSpace(email) == "" {
		return nil, nil
	}
	// The repo already returns (nil, nil) when no contact matches, so an
	// unknown sender flows through as a clean "no contact" rather than an error.
	return s.contactRepository.GetByEmailAndOrganization(ctx, *orgID, email)
}

func (s *contactService) ListSentEmails(ctx context.Context, userID, contactID uuid.UUID, limit int, beforeSentAt *time.Time, beforeTaskID *uuid.UUID) (*models.ContactSentEmailsResult, *errx.Error) {
	return s.contactRepository.ListSentEmails(ctx, userID, contactID, limit, beforeSentAt, beforeTaskID)
}

func (s *contactService) ListTimeline(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID, limit int, before *time.Time) (*models.ContactTimelineResult, *errx.Error) {
	return s.contactRepository.ListTimeline(ctx, userID, orgID, contactID, limit, before)
}
