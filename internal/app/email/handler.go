package email

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *emailService) Search(ctx context.Context, userID, search, cursor, tag, limit string) (*models.EmailsResult, *errx.Error) {
	cursorId, err := validate.Uuid(tag)
	if err != nil {
		return nil, err
	}
	tagId, err := validate.Uuid(tag)
	if err != nil {
		return nil, err
	}

	limitN, err := validate.Limit(limit)
	if err != nil {
		return nil, err
	}

	return s.emailRepository.Search(ctx, userID, search, cursorId, tagId, limitN)
}

func (s *emailService) Get(ctx context.Context, userID, emailAccountID string) (*models.Email, *errx.Error) {
	return s.emailRepository.Get(ctx, userID, emailAccountID)
}

func (s *emailService) Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error) {
	return s.emailRepository.Update(ctx, userID, emailAccountID, udata)
}

func (s *emailService) UpdateTrackingDomain(ctx context.Context, userID, emailAccountID, domain string) *errx.Error {
	return s.emailRepository.UpdateTrackingDomain(ctx, userID, emailAccountID, domain)
}

func (s *emailService) Delete(ctx context.Context, userID, emailAccountID string) *errx.Error {
	return s.emailRepository.Delete(ctx, userID, emailAccountID)
}
