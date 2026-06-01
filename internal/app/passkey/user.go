package passkey

import (
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// webAuthnUser adapts a Warmbly user plus their stored passkeys to the
// webauthn.User interface the engine needs for both ceremonies.
type webAuthnUser struct {
	user        *models.User
	credentials []webauthn.Credential
}

func newWebAuthnUser(user *models.User, creds []*models.WebAuthnCredential) *webAuthnUser {
	return &webAuthnUser{
		user:        user,
		credentials: toWebAuthnCredentials(creds),
	}
}

// WebAuthnID is the opaque, stable, PII-free user handle. The account UUID
// (16 raw bytes) fits the <=64-byte limit, never changes, and is exactly what
// a discoverable assertion returns as the userHandle for account resolution.
func (u *webAuthnUser) WebAuthnID() []byte {
	id := u.user.ID
	return id[:]
}

func (u *webAuthnUser) WebAuthnName() string { return u.user.Email }

func (u *webAuthnUser) WebAuthnDisplayName() string {
	name := strings.TrimSpace(u.user.FirstName + " " + u.user.LastName)
	if name == "" {
		return u.user.Email
	}
	return name
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// excludeList is the user's existing credentials as descriptors, passed to
// BeginRegistration so a device that already holds a passkey can't enroll a
// duplicate.
func (u *webAuthnUser) excludeList() []protocol.CredentialDescriptor {
	return webauthn.Credentials(u.credentials).CredentialDescriptors()
}

// toWebAuthnCredentials converts stored credentials into the engine's type.
func toWebAuthnCredentials(creds []*models.WebAuthnCredential) []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(creds))
	for _, c := range creds {
		out = append(out, webauthn.Credential{
			ID:                c.CredentialID,
			PublicKey:         c.PublicKey,
			AttestationType:   c.AttestationType,
			AttestationFormat: c.AttestationFormat,
			Transport:         toTransports(c.Transports),
			Flags: webauthn.CredentialFlags{
				UserPresent:    c.UserPresent,
				UserVerified:   c.UserVerified,
				BackupEligible: c.BackupEligible,
				BackupState:    c.BackupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:       c.AAGUID,
				SignCount:    c.SignCount,
				CloneWarning: c.CloneWarning,
			},
		})
	}
	return out
}

// credentialFromWebAuthn builds a persistable model from an engine credential.
func credentialFromWebAuthn(userID uuid.UUID, c *webauthn.Credential, name string) *models.WebAuthnCredential {
	// aaguid is a NOT NULL BYTEA column; a nil slice would encode as SQL NULL
	// and fail the insert, so coalesce to empty bytes.
	aaguid := c.Authenticator.AAGUID
	if aaguid == nil {
		aaguid = []byte{}
	}

	return &models.WebAuthnCredential{
		UserID:            userID,
		CredentialID:      c.ID,
		PublicKey:         c.PublicKey,
		AttestationType:   c.AttestationType,
		AttestationFormat: c.AttestationFormat,
		Transports:        fromTransports(c.Transport),
		AAGUID:            aaguid,
		SignCount:         c.Authenticator.SignCount,
		CloneWarning:      c.Authenticator.CloneWarning,
		BackupEligible:    c.Flags.BackupEligible,
		BackupState:       c.Flags.BackupState,
		UserPresent:       c.Flags.UserPresent,
		UserVerified:      c.Flags.UserVerified,
		Name:              name,
	}
}

func toTransports(in []string) []protocol.AuthenticatorTransport {
	out := make([]protocol.AuthenticatorTransport, 0, len(in))
	for _, t := range in {
		out = append(out, protocol.AuthenticatorTransport(t))
	}
	return out
}

func fromTransports(in []protocol.AuthenticatorTransport) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, string(t))
	}
	return out
}
