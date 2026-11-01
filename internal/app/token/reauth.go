package token

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// ReauthWindow is how long a re-authentication counts for.
//
// Long enough to complete the thing you re-authenticated in order to do (mint a
// key, register a passkey, hand over a workspace), short enough that walking
// away from an unlocked laptop does not leave the window open.
const ReauthWindow = 5 * time.Minute

// StampReauth records that this session just re-proved the account holder.
func (s *tokenService) StampReauth(ctx context.Context, sessionID uuid.UUID) *errx.Error {
	now := time.Now()
	if err := s.tokenRepository.StampReauth(ctx, sessionID, now); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	// Invalidate copies used by older processes during rolling deployments.
	if err := s.deleteSession(ctx, sessionID); err != nil {
		return err
	}
	return nil
}
