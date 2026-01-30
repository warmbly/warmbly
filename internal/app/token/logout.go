package token

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

func (s *tokenService) RevokeSession(ctx context.Context, accessToken string) *errx.Error {
	sess, err := s.ValidateAccessToken(ctx, accessToken)
	if err != nil {
		return err
	}

	now := time.Now()

	tx, xerr := s.db.Begin(ctx)
	if xerr != nil {
		db.CaptureError(err, "", nil, "begin")
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)

	if err := s.tokenRepository.RevokeSession(ctx, tx, sess.ID, now); err != nil {
		return err
	}

	if err := s.deleteSession(ctx, sess.ID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return errx.InternalError()
	}

	return nil
}

func (s *tokenService) RevokeAllSession(ctx context.Context, accessToken string) *errx.Error {
	sess, err := s.ValidateAccessToken(ctx, accessToken)
	if err != nil {
		return err
	}

	now := time.Now()

	tx, xerr := s.db.Begin(ctx)
	if xerr != nil {
		db.CaptureError(err, "", nil, "begin")
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)

	if err := s.tokenRepository.RevokeSession(ctx, tx, sess.ID, now); err != nil {
		return err
	}

	sess.RevokedAt = &now

	sessions, err := s.tokenRepository.FindExpiredSessions(ctx, sess.UserID, now.Truncate(SessionTTL))
	if err != nil {
		return err
	}

	for _, sess := range sessions {
		if err := s.deleteSession(ctx, sess); err != nil {
			return err
		}
	}

	if err := s.tokenRepository.RevokeSessions(ctx, sess.UserID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return errx.InternalError()
	}

	return nil
}
