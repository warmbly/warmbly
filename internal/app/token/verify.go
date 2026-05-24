package token

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *tokenService) GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error) {
	sess, err := s.getSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if sess != nil {
		return sess, nil
	}

	sess, err = s.tokenRepository.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if err := s.saveSession(ctx, sess, SessionTTL); err != nil {
		return nil, err
	}
	return sess, nil
}

func sameTokenIssueTime(a, b time.Time) bool {
	// JWT issued-at precision can differ from DB timestamp precision.
	// Compare at second precision to avoid false mismatches.
	return a.UTC().Truncate(time.Second).Equal(b.UTC().Truncate(time.Second))
}

func (s *tokenService) ValidateAccessToken(ctx context.Context, accessToken string) (*models.Session, *errx.Error) {
	t, err := s.VerifyToken(accessToken)
	if err != nil {
		return nil, err
	}

	if t.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrToken
	}

	session, err := s.GetSession(ctx, t.SessionID)
	if err != nil {
		return nil, err
	}

	if session.AccessNonce != t.Nonce || !sameTokenIssueTime(session.LastRefreshedAt, t.IssuedAt.Time) {
		return nil, errx.ErrToken
	}

	return session, nil
}
