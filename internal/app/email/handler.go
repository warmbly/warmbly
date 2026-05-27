package email

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *emailService) Search(ctx context.Context, userID, search, cursor, tag, limit string) (*models.EmailsResult, *errx.Error) {
	cursorId, err := validate.Uuid(cursor)
	if err != nil {
		return nil, err
	}
	tagId, err := validate.Uuid(tag)
	if err != nil {
		return nil, err
	}

	if limit == "" {
		limit = "50"
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
	account, err := s.emailRepository.Update(ctx, userID, emailAccountID, udata)
	if err != nil {
		return nil, err
	}

	s.syncWarmupPoolMembership(ctx, account)
	return account, nil
}

func (s *emailService) UpdateTrackingDomain(ctx context.Context, userID, emailAccountID, domain string) *errx.Error {
	return s.emailRepository.UpdateTrackingDomain(ctx, userID, emailAccountID, domain)
}

func (s *emailService) Delete(ctx context.Context, userID, emailAccountID string) *errx.Error {
	account, xerr := s.emailRepository.Get(ctx, userID, emailAccountID)
	if xerr != nil && xerr != errx.ErrNotFound {
		return xerr
	}

	if xerr := s.emailRepository.Delete(ctx, userID, emailAccountID); xerr != nil {
		return xerr
	}

	s.removeFromAllWarmupPools(ctx, account)

	if s.webhookService != nil && account != nil && account.OrganizationID != nil {
		_, _ = s.webhookService.Dispatch(ctx, *account.OrganizationID, models.WebhookEventEmailAccountRemoved, map[string]any{
			"email_account_id": account.ID,
			"email":            account.Email,
			"provider":         account.Provider,
		})
	}
	return nil
}

func (s *emailService) syncWarmupPoolMembership(ctx context.Context, account *models.Email) {
	if s.warmupService == nil || account == nil {
		return
	}

	if !s.shouldParticipateInWarmupPool(ctx, account) {
		s.removeFromAllWarmupPools(ctx, account)
		return
	}

	_ = s.warmupService.EnsurePoolMembership(ctx, account.ID, s.resolveWarmupPoolType(ctx, account))
}

func (s *emailService) removeFromAllWarmupPools(ctx context.Context, account *models.Email) {
	if s.warmupService == nil || account == nil {
		return
	}

	for _, poolType := range []string{"premium", "free"} {
		_ = s.warmupService.RemovePoolMembership(ctx, account.ID, poolType)
	}
}

func (s *emailService) shouldParticipateInWarmupPool(ctx context.Context, account *models.Email) bool {
	if account == nil || account.Warmup == nil || account.Status != "active" || account.OrganizationID == nil || s.featureGate == nil {
		return false
	}

	canWarmup, err := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
	return err == nil && canWarmup
}

func (s *emailService) resolveWarmupPoolType(ctx context.Context, account *models.Email) string {
	if account == nil {
		return "premium"
	}
	if account.WarmupPoolType != "" {
		return account.WarmupPoolType
	}
	if account.OrganizationID != nil && s.featureGate != nil {
		isPaid, err := s.featureGate.IsPaidOrganization(ctx, *account.OrganizationID)
		if err == nil && !isPaid {
			return "free"
		}
	}
	return "premium"
}
