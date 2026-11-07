package twofa

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// generateRecoveryCodes returns recoveryCount plaintext codes (xxxxx-xxxxx) plus
// their argon2 hashes (the only form stored).
func generateRecoveryCodes() (plain []string, hashes []string, err error) {
	for i := 0; i < recoveryCount; i++ {
		raw, e := crypt.RID(10)
		if e != nil {
			return nil, nil, e
		}
		code := strings.ToLower(raw[:5] + "-" + raw[5:])
		h, e := argon2.Hash(code)
		if e != nil {
			return nil, nil, e
		}
		plain = append(plain, code)
		hashes = append(hashes, h)
	}
	return plain, hashes, nil
}

// isRecoveryFormat distinguishes a recovery code (contains '-') from a 6-digit
// TOTP code.
func isRecoveryFormat(code string) bool {
	return strings.Contains(strings.TrimSpace(code), "-")
}

// tryConsumeRecoveryCode verifies a recovery code (constant-time argon2) and
// marks the matched one used (single-use).
func (s *service) tryConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, code string) *errx.Error {
	code = strings.ToLower(strings.TrimSpace(code))
	rows, err := s.repo.ListUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	var verifyErr error
	for _, rc := range rows {
		ok, verr := argon2.Verify(code, rc.CodeHash)
		if verr != nil {
			verifyErr = verr
			continue
		}
		if ok {
			if cerr := s.repo.ConsumeRecoveryCode(ctx, rc.ID); cerr != nil {
				if errors.Is(cerr, repository.ErrRecoveryCodeUsed) {
					return ErrInvalidCode()
				}
				errs.CaptureException(cerr)
				return errx.InternalError()
			}
			return nil
		}
	}
	if verifyErr != nil {
		errs.CaptureException(verifyErr)
		return errx.InternalError()
	}
	return ErrInvalidCode()
}
