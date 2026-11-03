package twofa

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// CreatePendingChallenge mints a short-lived single-use pending token bound to a
// fresh nonce in Redis. The token carries no AccessNonce + has no DB session, so
// it can never be used as an access token — only exchanged via VerifyLogin.
func (s *service) CreatePendingChallenge(ctx context.Context, userID uuid.UUID, authProvider string, link *models.UserIdentity) (string, int, *errx.Error) {
	if link != nil && s.linker == nil {
		return "", 0, errx.InternalError()
	}
	pend := &models.TwoFAPending{UserID: userID, AuthProvider: authProvider, LinkIdentity: link}
	sid := uuid.New()
	nonce, err := crypt.Nonce()
	if err != nil {
		return "", 0, errx.InternalError()
	}
	now := time.Now()
	pendTok, terr := s.tokens.GenerateTokenFor(token.PurposeTwoFAPending, userID, sid, "", nonce, now, now.Add(pendingTTL))
	if terr != nil {
		return "", 0, errx.InternalError()
	}
	pend.Nonce = nonce
	if err := s.savePending(ctx, sid, pend, pendingTTL); err != nil {
		return "", 0, errx.InternalError()
	}
	return pendTok, int(pendingTTL.Seconds()), nil
}

// VerifyLogin validates the pending token + code (TOTP or recovery) and, on
// success, mints a real session via the SAME path as a normal login.
func (s *service) VerifyLogin(ctx context.Context, pendingToken, code, ipaddr, userAgent string) (*models.Token, *errx.Error) {
	claims, xerr := s.tokens.VerifyTokenFor(token.PurposeTwoFAPending, pendingToken)
	if xerr != nil {
		return nil, errx.New(errx.BadRequest, "Invalid or expired session")
	}
	pend, err := s.getPending(ctx, claims.SessionID)
	if err != nil {
		return nil, errx.InternalError()
	}
	// Bind the token to its single-use Redis record (an access token's session id
	// has no 2fa_pending record, so it can't be replayed here).
	if pend == nil || pend.Nonce != claims.Nonce || pend.UserID != claims.UserID {
		return nil, errx.New(errx.BadRequest, "Invalid or expired session")
	}
	// Every budget is charged before the code is compared, so parallel guesses
	// cannot share a slot. The account budget spans every challenge it is sent.
	ok, xerr := s.reserveAttempt(ctx, pendingTriesKey(claims.SessionID), maxTries, pendingTTL)
	if xerr != nil {
		return nil, xerr
	}
	if !ok {
		s.deletePending(ctx, claims.SessionID)
		return nil, errx.New(errx.BadRequest, "Too many attempts, please sign in again")
	}
	if ok, xerr = s.ipAllowed(ctx, ipaddr); xerr == nil && ok {
		ok, xerr = s.reserveAttempt(ctx, userTriesKey(claims.UserID), userLimit, userWindow)
	}
	if xerr != nil || !ok {
		s.releaseAttempt(ctx, pendingTriesKey(claims.SessionID))
		if xerr != nil {
			return nil, xerr
		}
		return nil, errx.New(errx.BadRequest, "Too many attempts, try again later")
	}

	row, err := s.repo.Get(ctx, claims.UserID)
	if err != nil {
		s.releaseAttempt(ctx, userTriesKey(claims.UserID))
		s.releaseAttempt(ctx, pendingTriesKey(claims.SessionID))
		return nil, errx.InternalError()
	}
	if row == nil || !row.Enabled {
		s.releaseAttempt(ctx, userTriesKey(claims.UserID))
		s.deletePending(ctx, claims.SessionID)
		return nil, errx.New(errx.BadRequest, "2FA is not enabled")
	}

	if xerr := s.checkCode(ctx, claims.UserID, row, code); xerr != nil {
		return nil, xerr
	}
	_ = s.cache.Del(ctx, userTriesKey(claims.UserID)).Err()

	// Single-use: only the request that deletes the pending record mints a
	// session (delete-then-mint closes a double-spend race).
	if !s.deletePending(ctx, claims.SessionID) {
		return nil, errx.New(errx.BadRequest, "Invalid or expired session")
	}
	// A ban imposed while the challenge was open refuses it; an unreadable ban state refuses too.
	if s.users != nil {
		scope, berr := s.users.GetBanState(ctx, claims.UserID)
		if berr != nil {
			errs.CaptureException(berr)
			return nil, errx.InternalError()
		}
		if models.BanScope(scope).Has(models.BanScopeLogin) {
			return nil, errx.ErrAccountSuspended
		}
	}
	// A carried identity links only here, once both factors have passed.
	if pend.LinkIdentity != nil {
		if s.linker == nil {
			return nil, errx.InternalError()
		}
		if lerr := s.linker.Link(ctx, claims.UserID, *pend.LinkIdentity); lerr != nil {
			if errors.Is(lerr, repository.ErrIdentityTaken) {
				return nil, errx.New(errx.Forbidden, "that identity is already linked to another account")
			}
			errs.CaptureException(lerr)
			return nil, errx.InternalError()
		}
	}
	provider := token.AuthProviderEmail
	if pend.AuthProvider != "" {
		provider = pend.AuthProvider
	}
	// The password was checked before the challenge was minted and a TOTP or
	// recovery code has just been checked here, so this session has two factors.
	return s.tokens.GenerateMFASession(ctx, claims.UserID, "", ipaddr, userAgent, provider)
}
