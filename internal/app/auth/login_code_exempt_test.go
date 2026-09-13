package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/authrisk"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Only the one read loginCodeRequired makes is implemented; anything else
// would panic, which is the point.
type exemptUsers struct {
	repository.UserRepository
	exempt bool
	err    error
}

func (u exemptUsers) IsLoginCodeExempt(context.Context, uuid.UUID) (bool, error) {
	return u.exempt, u.err
}

func svc(users repository.UserRepository, mode string) *authService {
	return &authService{
		userRepository: users,
		mailDelivers:   true,
		policy:         &config.AuthPolicy{LoginCode: mode},
	}
}

// The reason this exists: a reviewer or auditor has to sign in and cannot read
// this instance's mail.
func TestExemptAccountSkipsTheLoginCode(t *testing.T) {
	for _, mode := range []string{config.LoginCodeNewDevice, config.LoginCodeAlways} {
		t.Run(mode, func(t *testing.T) {
			s := svc(exemptUsers{exempt: true}, mode)
			if s.loginCodeRequired(context.Background(), uuid.New(), "curl/8", authrisk.Verdict{}) {
				t.Errorf("an exempt account must not be asked for a code under %q", mode)
			}
		})
	}
}

// "always" is the mode that would otherwise make a vendor review impossible,
// so the exemption is checked before the policy rather than inside it.
func TestNonExemptAccountStillGetsTheCode(t *testing.T) {
	s := svc(exemptUsers{exempt: false}, config.LoginCodeAlways)
	if !s.loginCodeRequired(context.Background(), uuid.New(), "curl/8", authrisk.Verdict{}) {
		t.Error("a normal account must still be asked for a code")
	}
}

// A database that cannot answer is not an exemption. Failing open here would
// turn one bad query into a silent, instance-wide removal of the check.
func TestReadFailureIsNotAnExemption(t *testing.T) {
	s := svc(exemptUsers{exempt: true, err: errors.New("db is down")}, config.LoginCodeAlways)
	if !s.loginCodeRequired(context.Background(), uuid.New(), "curl/8", authrisk.Verdict{}) {
		t.Error("a failed lookup must still demand the code")
	}
}

// The exemption removes the emailed code and nothing else. A transport that
// cannot deliver still short-circuits first, because otherwise nobody could
// complete a login at all.
func TestUndeliverableMailStillShortCircuits(t *testing.T) {
	s := svc(exemptUsers{exempt: false}, config.LoginCodeAlways)
	s.mailDelivers = false
	if s.loginCodeRequired(context.Background(), uuid.New(), "curl/8", authrisk.Verdict{}) {
		t.Error("a deployment that cannot send mail must never demand a code")
	}
}

var _ = models.LoginCodeExemption{}
