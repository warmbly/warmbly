package auth

import (
	"context"
	"net/mail"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// emailUserRepo answers GetUserByEmail with one account and nothing else.
type emailUserRepo struct {
	repository.UserRepository
	user *models.User
}

func (r *emailUserRepo) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	if r.user != nil && r.user.Email == email {
		return r.user, nil
	}
	return nil, errx.ErrUser
}

// hashAuthRepo reports one password hash for every account.
type hashAuthRepo struct {
	repository.AuthRepository
	hash string
}

func (r *hashAuthRepo) GetPasswordHash(context.Context, uuid.UUID) (string, *errx.Error) {
	return r.hash, nil
}

// memoryIdentities records what got linked and knows one (issuer, subject).
type memoryIdentities struct {
	known   map[string]uuid.UUID
	linked  []models.UserIdentity
	touched int
	// taken makes Link answer as the repository does when another account
	// already holds the identity.
	taken bool
}

func (m *memoryIdentities) FindUserByIdentity(_ context.Context, issuer, subject string) (uuid.UUID, error) {
	return m.known[issuer+"|"+subject], nil
}

func (m *memoryIdentities) Link(_ context.Context, _ uuid.UUID, identity models.UserIdentity) error {
	if m.taken {
		return repository.ErrIdentityTaken
	}
	m.linked = append(m.linked, identity)
	return nil
}

func (m *memoryIdentities) HasIdentityForIssuer(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}

func (m *memoryIdentities) TouchLogin(context.Context, string, string) error {
	m.touched++
	return nil
}

func (m *memoryIdentities) ListForUser(context.Context, uuid.UUID) ([]models.UserIdentity, error) {
	return nil, nil
}

func federatedFixture(hash string, policy *config.AuthPolicy) (*authService, *models.User, *memoryIdentities) {
	u := &models.User{ID: uuid.New(), Email: "owner@example.com"}
	ids := &memoryIdentities{known: map[string]uuid.UUID{}}
	s := &authService{
		userRepository: &emailUserRepo{user: u},
		authRepository: &hashAuthRepo{hash: hash},
		identities:     ids,
		policy:         policy,
	}
	return s, u, ids
}

func resolve(t *testing.T, s *authService) federatedResolution {
	t.Helper()
	res, err := s.resolveFederatedUser(context.Background(), models.IdentityProviderGoogle,
		"https://accounts.google.com", "subject-1", &mail.Address{Address: "owner@example.com"}, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return res
}

// The address a provider asserts is the same address a password account was
// registered under; it is not proof of that account's password. An account
// that has one keeps the identity unlinked and the session unissued until it
// is presented.
func TestFederatedSignInParksOnAPasswordAccount(t *testing.T) {
	s, u, ids := federatedFixture("$argon2id$hash", &config.AuthPolicy{})

	res := resolve(t, s)
	if !res.LinkRequired {
		t.Fatal("an address match linked a password account without its password")
	}
	if res.UserID != u.ID {
		t.Fatalf("resolved %s, want %s", res.UserID, u.ID)
	}
	if len(ids.linked) != 0 {
		t.Fatalf("linked %v before the password arrived", ids.linked)
	}
	if res.Identity.Subject != "subject-1" || res.Identity.Provider != models.IdentityProviderGoogle {
		t.Fatalf("the parked identity is %+v", res.Identity)
	}
}

// An account without a password was created through a verified address (a
// provider, an invitation) and has nothing to ask for, so it links on the
// spot as it always has.
func TestFederatedSignInLinksAPasswordlessAccount(t *testing.T) {
	s, u, ids := federatedFixture("", &config.AuthPolicy{})

	res := resolve(t, s)
	if res.LinkRequired {
		t.Fatal("a passwordless account was asked for a password")
	}
	if res.UserID != u.ID || len(ids.linked) != 1 || ids.linked[0].Subject != "subject-1" {
		t.Fatalf("resolved %s with links %v", res.UserID, ids.linked)
	}
}

// With password sign-in off there is no password to present, and the identity
// provider is the gate the operator chose.
func TestFederatedSignInLinksWhenPasswordsAreOff(t *testing.T) {
	s, _, ids := federatedFixture("$argon2id$hash", &config.AuthPolicy{DisablePasswordLogin: true})

	res := resolve(t, s)
	if res.LinkRequired || len(ids.linked) != 1 {
		t.Fatalf("link_required=%v links=%v, want a direct link", res.LinkRequired, ids.linked)
	}
}

// A known (issuer, subject) is a re-login, and the password is never asked
// again for an identity the account already claimed.
func TestFederatedSignInReturnsAKnownIdentityWithoutAPassword(t *testing.T) {
	s, u, ids := federatedFixture("$argon2id$hash", &config.AuthPolicy{})
	ids.known["https://accounts.google.com|subject-1"] = u.ID

	res := resolve(t, s)
	if res.LinkRequired || res.UserID != u.ID {
		t.Fatalf("link_required=%v user=%s, want a plain sign-in as %s", res.LinkRequired, res.UserID, u.ID)
	}
	if ids.touched != 1 || len(ids.linked) != 0 {
		t.Fatalf("touched=%d links=%v", ids.touched, ids.linked)
	}
}

// A password prompt that gates nothing would recur on every sign-in. Without
// an identity store, or without a subject to store, there is nothing to link
// and the sign-in completes as it did before.
func TestFederatedSignInAsksNothingWhenNothingCanBeLinked(t *testing.T) {
	s, u, _ := federatedFixture("$argon2id$hash", &config.AuthPolicy{})
	s.identities = nil

	res := resolve(t, s)
	if res.LinkRequired || res.UserID != u.ID {
		t.Fatalf("link_required=%v user=%s with no identity store", res.LinkRequired, res.UserID)
	}

	s, u, ids := federatedFixture("$argon2id$hash", &config.AuthPolicy{})
	res, err := s.resolveFederatedUser(context.Background(), models.IdentityProviderGoogle,
		"https://accounts.google.com", "", &mail.Address{Address: "owner@example.com"}, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.LinkRequired || res.UserID != u.ID || len(ids.linked) != 0 {
		t.Fatalf("link_required=%v user=%s links=%v with an empty subject", res.LinkRequired, res.UserID, ids.linked)
	}
}

// An identity another account already holds is refused, and nobody is signed
// in on it: the repository reports the foreign owner and the sign-in stops.
func TestFederatedSignInRefusesAnIdentityAnotherAccountHolds(t *testing.T) {
	s, _, ids := federatedFixture("", &config.AuthPolicy{})
	ids.taken = true

	_, err := s.resolveFederatedUser(context.Background(), models.IdentityProviderGoogle,
		"https://accounts.google.com", "subject-1", &mail.Address{Address: "owner@example.com"}, "", "")
	if err == nil || err.Code != errx.Forbidden {
		t.Fatalf("got %v, want a Forbidden refusal", err)
	}
}

func (m *memoryIdentities) Unlink(context.Context, uuid.UUID, string) error { return nil }
