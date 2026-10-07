package cloudlink

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type reconciliationKey struct{}

func (s *service) revokeAccountConsents(ctx context.Context, orgID, accountID uuid.UUID, m *models.CloudLinkMailbox) *errx.Error {
	r, ok := s.repo.(repository.CloudManagedConsentRepository)
	if !ok {
		return nil
	}
	if m != nil {
		if err := r.RevokeManagedConsents(ctx, m.InstanceID, &accountID); err != nil {
			return errx.InternalError()
		}
		return nil
	}
	seen := map[uuid.UUID]bool{}
	for _, org := range []*uuid.UUID{&orgID, nil} {
		l, err := s.repo.Get(ctx, org)
		if err != nil {
			return errx.InternalError()
		}
		if l != nil && !seen[l.InstanceID] {
			if err := r.RevokeManagedConsents(ctx, l.InstanceID, &accountID); err != nil {
				return errx.InternalError()
			}
			seen[l.InstanceID] = true
		}
	}
	return nil
}

func reconciliationLocked(ctx context.Context) bool { return ctx.Value(reconciliationKey{}) != nil }

func (s *service) reconcileLocked(ctx context.Context, fn func(context.Context) *errx.Error) *errx.Error {
	err := s.repo.WithReconciliationLock(ctx, func() error {
		xerr := fn(context.WithValue(ctx, reconciliationKey{}, true))
		if xerr != nil {
			return xerr
		}
		return nil
	})
	if err == nil {
		return nil
	}
	var xerr *errx.Error
	if errors.As(err, &xerr) {
		return xerr
	}
	return errx.InternalError()
}

// A removal intent survives either a lost acknowledgment or a failed local cleanup.
func (s *service) releaseEnrollment(ctx context.Context, l *models.CloudLink, m models.CloudLinkMailbox) *errx.Error {
	if l != nil {
		if r, ok := s.repo.(repository.CloudManagedConsentRepository); ok {
			if err := r.RevokeManagedConsents(ctx, l.InstanceID, &m.EmailAccountID); err != nil {
				return errx.InternalError()
			}
		}
	}
	if err := s.repo.BeginRemoval(ctx, m.EmailAccountID); err != nil {
		return errx.InternalError()
	}
	if err := s.carryStanding(ctx, m); err != nil {
		return errx.InternalError()
	}
	if l == nil {
		return ErrNotConnected
	}
	xerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+m.RemoteID.String(), nil, nil)
	if xerr != nil && xerr.Identifier != "pool_link_mailbox_not_found" && !linkAlreadyGone(xerr) {
		return xerr
	}
	if err := s.repo.Unenroll(ctx, m.EmailAccountID); err != nil {
		return errx.InternalError()
	}
	s.forgetToken(m.EmailAccountID)
	s.syncLocalPool(ctx, m.EmailAccountID)
	return nil
}

func (s *service) reconcileEnrollments(ctx context.Context, l *models.CloudLink, rows []models.CloudLinkMailbox) *errx.Error {
	for _, m := range rows {
		switch m.EnrollmentState {
		case "pending_remove":
			if m.Managed {
				acc, xerr := s.emails.GetByID(ctx, m.EmailAccountID)
				if xerr != nil {
					return xerr
				}
				if acc.OrganizationID == nil {
					return errx.InternalError()
				}
				if xerr := s.removeManaged(ctx, acc.OrganizationID.String(), &m); xerr != nil {
					return xerr
				}
			} else if xerr := s.releaseEnrollment(ctx, l, m); xerr != nil {
				return xerr
			}
		case "pending_enroll":
			acc, xerr := s.emails.GetByID(ctx, m.EmailAccountID)
			if xerr != nil {
				return xerr
			}
			if acc.OrganizationID == nil || acc.Status != "active" {
				if xerr := s.releaseEnrollment(ctx, l, m); xerr != nil {
					return xerr
				}
				continue
			}
			if _, xerr := s.Enroll(ctx, *acc.OrganizationID, m.EmailAccountID); xerr != nil {
				return xerr
			}
		}
	}
	return nil
}
