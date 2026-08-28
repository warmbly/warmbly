package auth

import (
	"context"
	"net/mail"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// recordingUserRepo captures the signup metadata write. Everything else is the
// minimum the account path touches.
type recordingUserRepo struct {
	repository.UserRepository
	created    *models.User
	gotIP      string
	gotUA      string
	gotRisk    int
	gotNorm    string
	recordHits int
}

func (r *recordingUserRepo) CreateUser(_ context.Context, email *mail.Address, _ string) (*models.User, error) {
	r.created = &models.User{ID: uuid.New(), Email: email.Address}
	return r.created, nil
}

func (r *recordingUserRepo) RecordSignupMetadata(_ context.Context, _ uuid.UUID, ip, ua string, risk int, normalized string) error {
	r.recordHits++
	r.gotIP, r.gotUA, r.gotRisk, r.gotNorm = ip, ua, risk, normalized
	return nil
}

type noopUserService struct{ user.UserService }

func (noopUserService) SaveUser(context.Context, *models.User) *errx.Error { return nil }

// The failure this guards against is the one that keeps recurring: a scorer
// that works perfectly and a call site that never runs. Testing signuprisk
// directly cannot see it; this exercises the account path itself.
func TestCreateAccountRecordsTheSignupOrigin(t *testing.T) {
	repo := &recordingUserRepo{}
	svc := &authService{userRepository: repo, userService: noopUserService{}}

	origin := SignupOrigin{IP: "203.0.113.5", UserAgent: "Mozilla/5.0 (test)"}
	if err := svc.createAccount(context.Background(), "Ada.Lovelace+signup@gmail.com", "hash", "", "", origin); err != nil {
		t.Fatalf("createAccount: %v", err.Message)
	}

	if repo.recordHits != 1 {
		t.Fatalf("signup metadata written %d times, want exactly once", repo.recordHits)
	}
	if repo.gotIP != origin.IP {
		t.Errorf("ip = %q, want %q", repo.gotIP, origin.IP)
	}
	if repo.gotUA != origin.UserAgent {
		t.Errorf("user agent = %q, want %q", repo.gotUA, origin.UserAgent)
	}
	// Normalized so the same person opening a second tagged account is visible.
	if repo.gotNorm != "adalovelace@gmail.com" {
		t.Errorf("normalized = %q, want the plus-tag and dots collapsed", repo.gotNorm)
	}
	if repo.gotRisk == 0 {
		t.Error("a tagged free-provider signup scored 0; the scorer is not reaching the write")
	}
}

// A signup with nothing notable still records its origin: the correlation data
// is the point, and only recording risky ones would leave the clusters #148
// looks for half-empty.
func TestCreateAccountRecordsACleanSignupToo(t *testing.T) {
	repo := &recordingUserRepo{}
	svc := &authService{userRepository: repo, userService: noopUserService{}}

	if err := svc.createAccount(context.Background(), "ada@acme.com", "hash", "", "", SignupOrigin{IP: "203.0.113.9"}); err != nil {
		t.Fatalf("createAccount: %v", err.Message)
	}
	if repo.recordHits != 1 {
		t.Fatalf("clean signup wrote metadata %d times, want once", repo.recordHits)
	}
	if repo.gotRisk != 0 {
		t.Errorf("risk = %d, want 0 for an ordinary business signup", repo.gotRisk)
	}
	if repo.gotNorm != "ada@acme.com" {
		t.Errorf("normalized = %q", repo.gotNorm)
	}
}
