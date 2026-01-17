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

	if session.AccessNonce != t.Nonce || !session.LastRefreshedAt.Equal(t.IssuedAt.Time) {
		return nil, errx.ErrToken
	}

	return session, nil
}
