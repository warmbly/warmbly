package twofa

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
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
// marks the matched one used (single-use). Returns false if none match.
func (s *service) tryConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	rows, err := s.repo.ListUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return false
	}
	for _, rc := range rows {
		if ok, verr := argon2.Verify(code, rc.CodeHash); verr == nil && ok {
			if cerr := s.repo.ConsumeRecoveryCode(ctx, rc.ID); cerr != nil {
				return false
			}
			return true
		}
	}
	return false
}
