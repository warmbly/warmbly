package twofa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/repository"
)

type verificationRepo struct {
	repository.TOTPRepository
	row                             *models.UserTOTP
	getErr, consumeErr, recoveryErr error
	lastStep                        uint64
	codes                           []models.RecoveryCode
}

func (r *verificationRepo) Get(context.Context, uuid.UUID) (*models.UserTOTP, error) {
	return r.row, r.getErr
}

func (r *verificationRepo) ConsumeTOTPStep(_ context.Context, _ uuid.UUID, step uint64, sealedSecret string) (bool, error) {
	if r.consumeErr != nil {
		return false, r.consumeErr
	}
	if sealedSecret != r.row.SecretSealed {
		return false, repository.ErrTOTPEnrollmentChanged
	}
	if step <= r.lastStep {
		return false, nil
	}
	r.lastStep = step
	return true, nil
}

func (r *verificationRepo) ListUnusedRecoveryCodes(context.Context, uuid.UUID) ([]models.RecoveryCode, error) {
	return r.codes, r.recoveryErr
}

func (r *verificationRepo) ConsumeRecoveryCode(context.Context, uuid.UUID) error {
	return r.consumeErr
}

func verificationFixture(t *testing.T) (*service, *verificationRepo, string) {
	t.Helper()
	secret := "JBSWY3DPEHPK3PXP"
	key := DeriveKey("verification-test-key")
	sealed, err := Seal(key, secret)
	if err != nil {
		t.Fatal(err)
	}
	r := &verificationRepo{row: &models.UserTOTP{UserID: uuid.New(), Enabled: true, SecretSealed: sealed}}
	s := &service{repo: r, sealKey: key}
	code, err := hotp(secret, uint64(time.Now().Unix())/totpPeriod)
	if err != nil {
		t.Fatal(err)
	}
	return s, r, code
}

func TestAuthenticatorVerificationDistinguishesReplayFromMismatch(t *testing.T) {
	s, r, code := verificationFixture(t)
	uid := r.row.UserID
	if ok, xerr := s.VerifyCurrentCode(t.Context(), uid, code); !ok || xerr != nil {
		t.Fatalf("first use: accepted=%t error=%v", ok, xerr)
	}
	if ok, xerr := s.VerifyCurrentCode(t.Context(), uid, code); ok || xerr == nil || xerr.Identifier != UsedCodeID {
		t.Fatalf("replay: accepted=%t error=%v", ok, xerr)
	}
	if ok, xerr := s.VerifyCurrentCode(t.Context(), uid, "invalid"); ok || xerr != nil {
		t.Fatalf("mismatch: accepted=%t error=%v", ok, xerr)
	}
}

func TestAuthenticatorVerificationRequiresAvailableStorageAndKeys(t *testing.T) {
	for _, failure := range []string{"read", "key", "consume", "recovery_read"} {
		t.Run(failure, func(t *testing.T) {
			s, r, code := verificationFixture(t)
			switch failure {
			case "read":
				r.getErr = errors.New("read unavailable")
			case "key":
				s.sealKey = DeriveKey("different-test-key")
			case "consume":
				r.consumeErr = errors.New("write unavailable")
			case "recovery_read":
				r.recoveryErr = errors.New("recovery unavailable")
				code = "abcde-fghij"
			}
			if ok, xerr := s.VerifyCurrentCode(t.Context(), r.row.UserID, code); ok || xerr == nil || xerr.Code != errx.Internal {
				t.Fatalf("verification requires available evidence: accepted=%t error=%v", ok, xerr)
			}
		})
	}
}

func TestRecoveryVerificationDistinguishesConcurrentUseFromStorageFailure(t *testing.T) {
	hash, err := argon2.Hash("abcde-fghij")
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []error{repository.ErrRecoveryCodeUsed, errors.New("write unavailable")} {
		s, r, _ := verificationFixture(t)
		r.codes = []models.RecoveryCode{{ID: uuid.New(), CodeHash: hash}}
		r.consumeErr = failure
		xerr := s.checkCode(t.Context(), r.row.UserID, r.row, "abcde-fghij")
		if xerr == nil {
			t.Fatal("a recovery code must be consumed before it is accepted")
		}
		if errors.Is(failure, repository.ErrRecoveryCodeUsed) {
			if xerr.Identifier != InvalidCodeID {
				t.Fatalf("concurrent use: %v", xerr)
			}
		} else if xerr.Code != errx.Internal {
			t.Fatalf("storage failure: %v", xerr)
		}
	}
}

func TestRecoveryVerificationRequiresReadableHashesOrMatchingCode(t *testing.T) {
	hash, err := argon2.Hash("abcde-fghij")
	if err != nil {
		t.Fatal(err)
	}
	for _, matching := range []bool{false, true} {
		s, r, _ := verificationFixture(t)
		r.codes = []models.RecoveryCode{{ID: uuid.New(), CodeHash: "unreadable"}}
		if matching {
			r.codes = append(r.codes, models.RecoveryCode{ID: uuid.New(), CodeHash: hash})
		}
		ok, xerr := s.VerifyCurrentCode(t.Context(), r.row.UserID, "abcde-fghij")
		if matching {
			if !ok || xerr != nil {
				t.Fatalf("readable matching code: accepted=%t error=%v", ok, xerr)
			}
		} else if ok || xerr == nil || xerr.Code != errx.Internal {
			t.Fatalf("unreadable evidence: accepted=%t error=%v", ok, xerr)
		}
	}
}

func TestAuthenticatorVerificationRequiresCurrentEnrollment(t *testing.T) {
	s, r, code := verificationFixture(t)
	r.consumeErr = repository.ErrTOTPEnrollmentChanged
	if ok, xerr := s.VerifyCurrentCode(t.Context(), r.row.UserID, code); ok || xerr != nil {
		t.Fatalf("changed enrollment: accepted=%t error=%v", ok, xerr)
	}
}
