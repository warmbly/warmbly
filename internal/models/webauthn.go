package models

import (
	"time"

	"github.com/google/uuid"
)

// WebAuthnCredential is a stored passkey (FIDO2/WebAuthn credential).
//
// Sensitive material (credential ID, public key, AAGUID, counters) is kept
// off the JSON wire by default — the passkey manager renders from a purpose
// built view DTO instead of serializing this model directly.
type WebAuthnCredential struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"-"`
	CredentialID      []byte     `json:"-"`
	PublicKey         []byte     `json:"-"`
	AttestationType   string     `json:"-"`
	AttestationFormat string     `json:"-"`
	Transports        []string   `json:"transports"`
	AAGUID            []byte     `json:"-"`
	SignCount         uint32     `json:"-"`
	CloneWarning      bool       `json:"-"`
	BackupEligible    bool       `json:"backup_eligible"`
	BackupState       bool       `json:"backup_state"`
	UserPresent       bool       `json:"-"`
	UserVerified      bool       `json:"-"`
	Name              string     `json:"name"`
	CreatedAt         time.Time  `json:"created_at"`
	LastUsedAt        *time.Time `json:"last_used_at"`
}
