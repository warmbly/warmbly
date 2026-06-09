package twofa

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
)

// EnrollStart generates a fresh secret (sealed at rest immediately, enabled=false
// so login is unaffected) and returns the plaintext secret + provisioning URI
// ONCE so the user can scan/type it.
func (s *service) EnrollStart(ctx context.Context, userID uuid.UUID) (*EnrollStart, *errx.Error) {
	// Never let a fresh enrollment silently overwrite (and thus disable) an
	// already-active 2FA — disabling requires proof of a current code.
	if existing, gerr := s.repo.Get(ctx, userID); gerr == nil && existing != nil && existing.Enabled {
		return nil, errx.New(errx.BadRequest, "2FA is already enabled — disable it first")
	}
	user, err := s.users.GetUser(ctx, userID)
	if err != nil || user == nil {
		return nil, errx.InternalError()
	}
	secret, err := GenerateSecret()
	if err != nil {
		return nil, errx.InternalError()
	}
	sealed, err := Seal(s.sealKey, secret)
	if err != nil {
		return nil, errx.InternalError()
	}
	if err := s.repo.UpsertPending(ctx, userID, sealed); err != nil {
		return nil, errx.InternalError()
	}
	return &EnrollStart{Secret: secret, OtpauthURI: OtpauthURI(issuer, user.Email, secret)}, nil
}

// EnrollConfirm verifies a code against the pending secret, enables 2FA, and
// returns fresh recovery codes (plaintext, ONCE — stored hashed).
func (s *service) EnrollConfirm(ctx context.Context, userID uuid.UUID, code string) ([]string, *errx.Error) {
	row, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if row == nil {
		return nil, errx.New(errx.BadRequest, "Start enrollment first")
	}
	if row.Enabled {
		return nil, errx.New(errx.BadRequest, "2FA is already enabled")
	}
	secret, err := Open(s.sealKey, row.SecretSealed)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !ValidateCode(secret, code) {
		return nil, errx.New(errx.BadRequest, "Invalid code")
	}
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		return nil, errx.InternalError()
	}
	if err := s.repo.InsertRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.repo.Enable(ctx, userID); err != nil {
		return nil, errx.InternalError()
	}
	return codes, nil
}
