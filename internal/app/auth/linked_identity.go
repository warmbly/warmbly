package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// LinkedIdentity is a provider the account can sign in with, without the
// issuer and subject that bind it.
type LinkedIdentity struct {
	Provider    string     `json:"provider"`
	Label       string     `json:"label"`
	Email       string     `json:"email,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

// ErrIdentityNotFound answers a removal of a provider the account has not linked.
var ErrIdentityNotFound = errx.NewWithIdentifier(errx.NotFound, "identity_not_found",
	"That sign-in method is not linked to your account.")

var identityLabels = map[string]string{
	models.IdentityProviderGoogle: "Google",
	models.IdentityProviderApple:  "Apple",
	models.IdentityProviderOIDC:   "Single sign-on",
}

func (s *authService) LinkedIdentities(ctx context.Context, userID uuid.UUID) ([]LinkedIdentity, *errx.Error) {
	out := []LinkedIdentity{}
	if s.identities == nil {
		return out, nil
	}
	linked, err := s.identities.ListForUser(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	labels := s.FederatedProviderLabels()
	for _, id := range linked {
		label := labels[id.Provider]
		if label == "" {
			label = identityLabels[id.Provider]
		}
		out = append(out, LinkedIdentity{
			Provider:    id.Provider,
			Label:       label,
			Email:       id.Email,
			CreatedAt:   id.CreatedAt,
			LastLoginAt: id.LastLoginAt,
		})
	}
	return out, nil
}

// ErrIdentitySessionsNotEnded answers a removal whose devices signed in with
// the provider could not all be signed out; repeating the request finishes it.
var ErrIdentitySessionsNotEnded = &errx.Error{Code: errx.ServiceUnavailable, Identifier: "sessions_not_ended", RetryAfter: 5,
	Message: "The sign-in method was removed, but the devices that signed in with it could not all be signed out. Wait a few seconds and try again."}

// UnlinkIdentity removes provider from the user's sign-in methods and ends
// every other session that signed in through it. A repeat converges, so a
// failed sign-out is finished by retrying.
func (s *authService) UnlinkIdentity(ctx context.Context, userID, currentSessionID uuid.UUID, provider string) *errx.Error {
	if s.identities == nil || identityLabels[provider] == "" {
		return ErrIdentityNotFound
	}
	err := s.identities.Unlink(ctx, userID, provider)
	switch {
	case err == nil, errors.Is(err, repository.ErrIdentityNotLinked):
	case errors.Is(err, repository.ErrLastSignInMethod):
		return errx.ErrLastSignInMethod
	default:
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if s.tokenService == nil {
		return nil
	}
	sessions, xerr := s.tokenService.ListSessions(ctx, userID, currentSessionID)
	if xerr != nil {
		errs.CaptureException(xerr)
		return ErrIdentitySessionsNotEnded
	}
	via := sessionProvider(provider)
	for _, sess := range sessions {
		if sess.Current || sess.AuthProvider != via {
			continue
		}
		id, perr := uuid.Parse(sess.ID)
		if perr != nil {
			continue
		}
		if xerr := s.tokenService.RevokeSessionByID(ctx, userID, id, currentSessionID); xerr != nil && xerr != errx.ErrSessionNotFound {
			errs.CaptureException(xerr)
			return ErrIdentitySessionsNotEnded
		}
	}
	return nil
}
