package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type identityStub struct {
	owners map[string]uuid.UUID
	linked map[uuid.UUID][]models.UserIdentity
}

func (r *identityStub) FindUserByIdentity(ctx context.Context, issuer, subject string) (uuid.UUID, error) {
	return r.owners[issuer+"|"+subject], nil
}
func (r *identityStub) Link(ctx context.Context, userID uuid.UUID, identity models.UserIdentity) error {
	return errors.New("re-authentication must never link an identity")
}
func (r *identityStub) HasIdentityForIssuer(ctx context.Context, userID uuid.UUID, issuer string) (bool, error) {
	return false, nil
}
func (r *identityStub) TouchLogin(ctx context.Context, issuer, subject string) error { return nil }
func (r *identityStub) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.UserIdentity, error) {
	return r.linked[userID], nil
}

// banUsers reports a ban scope for every user.
type banUsers struct {
	repository.UserRepository
	scope uint32
}

func (u banUsers) GetBanState(context.Context, uuid.UUID) (uint32, error) { return u.scope, nil }

func (r *identityStub) Unlink(context.Context, uuid.UUID, string) error { return nil }

type unlinkStub struct {
	identityStub
	err error
}

func (u *unlinkStub) Unlink(context.Context, uuid.UUID, string) error { return u.err }

// A refusal keeps its own code, and a provider already gone converges to done.
func TestUnlinkIdentityMapsRefusals(t *testing.T) {
	cases := map[error]*errx.Error{
		nil:                             nil,
		repository.ErrIdentityNotLinked: nil,
		repository.ErrLastSignInMethod:  errx.ErrLastSignInMethod,
	}
	for in, want := range cases {
		s := &authService{identities: &unlinkStub{err: in}}
		if got := s.UnlinkIdentity(context.Background(), uuid.New(), uuid.New(), "apple"); got != want {
			t.Fatalf("%v: got %v, want %v", in, got, want)
		}
	}
	s := &authService{identities: &unlinkStub{}}
	if got := s.UnlinkIdentity(context.Background(), uuid.New(), uuid.New(), "github"); got != ErrIdentityNotFound {
		t.Fatalf("unknown provider: got %v, want ErrIdentityNotFound", got)
	}
}

type sessionsStub struct {
	token.TokenService
	sessions []token.SessionView
	revoked  []string
}

func (t *sessionsStub) ListSessions(context.Context, uuid.UUID, uuid.UUID) ([]token.SessionView, *errx.Error) {
	return t.sessions, nil
}

func (t *sessionsStub) RevokeSessionByID(_ context.Context, _, id, _ uuid.UUID) *errx.Error {
	t.revoked = append(t.revoked, id.String())
	return nil
}

// Removing a provider signs out the other devices that signed in through it, and only those.
func TestUnlinkIdentityEndsSessionsFromThatProvider(t *testing.T) {
	current, viaApple, viaGoogle := uuid.New(), uuid.New(), uuid.New()
	tokens := &sessionsStub{sessions: []token.SessionView{
		{ID: current.String(), Current: true, AuthProvider: token.AuthProviderApple},
		{ID: viaApple.String(), AuthProvider: token.AuthProviderApple},
		{ID: viaGoogle.String(), AuthProvider: token.AuthProviderGoogle},
	}}
	s := &authService{identities: &unlinkStub{}, tokenService: tokens}
	if err := s.UnlinkIdentity(context.Background(), uuid.New(), current, models.IdentityProviderApple); err != nil {
		t.Fatal(err)
	}
	if len(tokens.revoked) != 1 || tokens.revoked[0] != viaApple.String() {
		t.Fatalf("revoked %v, want only %s", tokens.revoked, viaApple)
	}
}
