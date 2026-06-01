package passkey

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/getsentry/sentry-go"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
)

func (s *service) BeginRegistration(ctx context.Context, userID uuid.UUID) (*protocol.CredentialCreation, *errx.Error) {
	wuser, xerr := s.loadUser(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	// Require a discoverable (resident) key so the passkey works for
	// usernameless login, and exclude already-enrolled credentials so a
	// device can't register a duplicate.
	options, sessionData, err := s.web.BeginRegistration(
		wuser,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(wuser.excludeList()),
	)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.ErrPasskey
	}

	if xerr := s.saveSession(ctx, registrationKey(userID), sessionData); xerr != nil {
		return nil, xerr
	}

	return options, nil
}

func (s *service) FinishRegistration(ctx context.Context, userID uuid.UUID, name string, credential []byte) (*CredentialView, *errx.Error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxPasskeyName {
		return nil, errx.ErrPasskeyName
	}

	sessionData, xerr := s.takeSession(ctx, registrationKey(userID))
	if xerr != nil {
		return nil, xerr
	}

	wuser, xerr := s.loadUser(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return nil, errx.ErrPasskey
	}

	cred, err := s.web.CreateCredential(wuser, *sessionData, parsed)
	if err != nil {
		return nil, errx.ErrPasskey
	}

	if name == "" {
		name = defaultName(cred)
	}

	model := credentialFromWebAuthn(userID, cred, name)
	if xerr := s.repo.CreateCredential(ctx, model); xerr != nil {
		return nil, xerr
	}

	return toView(model), nil
}

// loadUser builds the engine user (account + stored credentials) for a known
// user id.
func (s *service) loadUser(ctx context.Context, userID uuid.UUID) (*webAuthnUser, *errx.Error) {
	u, err := s.userRepo.GetUser(ctx, userID)
	if err != nil || u == nil {
		return nil, errx.ErrUser
	}

	creds, xerr := s.repo.ListByUser(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	return newWebAuthnUser(u, creds), nil
}

// defaultName picks a friendly label when the client didn't supply one,
// preferring the AAGUID-derived provider name (e.g. "iCloud Keychain").
func defaultName(cred *webauthn.Credential) string {
	if name := providerName(cred.Authenticator.AAGUID); name != "" {
		return name
	}
	return "Passkey"
}
