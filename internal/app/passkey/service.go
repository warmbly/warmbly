// Package passkey implements the WebAuthn (passkey) ceremonies and credential
// management for Warmbly. It is a control-plane service: passkeys are stored
// in Postgres and a passkey sign-in mints a normal session via the token
// service, exactly like password or OAuth login.
//
// Passkey login is discoverable (usernameless): the browser surfaces a passkey
// in autofill, the assertion carries the user handle (the account's UUID), and
// we resolve the account from it — no email/password step, no email OTP.
package passkey

import (
	"context"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service drives the passkey ceremonies and credential management.
type Service interface {
	// BeginRegistration starts enrolling a new passkey for an authenticated
	// user. The returned options are sent to navigator.credentials.create().
	BeginRegistration(ctx context.Context, userID uuid.UUID) (*protocol.CredentialCreation, *errx.Error)
	// FinishRegistration verifies the attestation and persists the passkey.
	FinishRegistration(ctx context.Context, userID uuid.UUID, name string, credential []byte) (*CredentialView, *errx.Error)

	// BeginLogin starts a discoverable (usernameless) sign-in. The returned
	// session correlates the challenge with FinishLogin.
	BeginLogin(ctx context.Context) (*LoginChallenge, *errx.Error)
	// FinishLogin verifies the assertion, resolves the account from the user
	// handle, and mints a full session — single step, no email OTP.
	FinishLogin(ctx context.Context, session string, credential []byte, ipaddr, userAgent string) (*models.Token, *errx.Error)

	// ListCredentials returns the user's passkeys for the manager UI.
	ListCredentials(ctx context.Context, userID uuid.UUID) ([]*CredentialView, *errx.Error)
	// RenameCredential updates a passkey's friendly name.
	RenameCredential(ctx context.Context, userID, id uuid.UUID, name string) (*CredentialView, *errx.Error)
	// DeleteCredential removes a passkey.
	DeleteCredential(ctx context.Context, userID, id uuid.UUID) *errx.Error
}

type service struct {
	web      *webauthn.WebAuthn
	repo     repository.WebAuthnRepository
	userRepo repository.UserRepository
	token    token.TokenService
	cache    *cache.Cache
}

// Deps is the dependency set for the passkey service.
type Deps struct {
	Repo         repository.WebAuthnRepository
	UserRepo     repository.UserRepository
	TokenService token.TokenService
	Cache        *cache.Cache

	RPID          string
	RPDisplayName string
	RPOrigins     []string
}

// New constructs the passkey service and the underlying WebAuthn engine.
// Registration defaults to discoverable, synced passkeys (resident key
// required, user verification preferred) with no attestation — the broadest
// provider compatibility for consumer passkeys.
func New(deps Deps) (Service, error) {
	web, err := webauthn.New(&webauthn.Config{
		RPID:          deps.RPID,
		RPDisplayName: deps.RPDisplayName,
		RPOrigins:     deps.RPOrigins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
	if err != nil {
		return nil, err
	}

	return &service{
		web:      web,
		repo:     deps.Repo,
		userRepo: deps.UserRepo,
		token:    deps.TokenService,
		cache:    deps.Cache,
	}, nil
}

// LoginChallenge is returned to the browser to start a discoverable login.
// Options marshals to { publicKey: {...} }; the browser passes options.publicKey
// to startAuthentication.
type LoginChallenge struct {
	Session string                        `json:"session"`
	Options *protocol.CredentialAssertion `json:"options"`
}
