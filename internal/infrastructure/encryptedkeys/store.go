// Package encryptedkeys stores per-organization envelope-encrypted DEKs.
//
// A DEK (data encryption key) is a 32-byte AES-256 key generated per
// organization by the kms.Provider. The KMS encrypts it; the encrypted form
// ("blob") is what this package stores. Workers and backend services never
// store the plaintext DEK on disk; they fetch the blob, ask KMS to decrypt it,
// and hold the plaintext in a short-TTL Redis cache.
//
// Keys are organization-scoped because the things they seal — mailbox
// credentials, integration OAuth tokens, message content — are organization
// assets. Platform-level secrets (worker SSH keys, profile credentials) are
// stored under the zero UUID, which no real organization uses.
//
// Durability matters absolutely. Losing an encrypted DEK is unrecoverable —
// every encrypted mailbox credential, OAuth refresh token, and stored message
// for that organization becomes permanently unreadable. Implementations must
// therefore be backed by storage with strong durability guarantees (Postgres
// or equivalent). NATS JetStream KV is intentionally not offered here.
//
// Workers MUST NOT connect to Postgres directly (per CLAUDE.md), so the worker
// process uses the HTTP implementation, which calls a backend endpoint that
// in turn talks to the chosen durable store.
package encryptedkeys

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrAlreadyExists is returned by Put when a DEK is already stored for the
// given organization. Overwriting would silently invalidate every prior
// ciphertext; rotation must use a separate explicit code path that re-encrypts
// existing data under the new DEK.
var ErrAlreadyExists = errors.New("encryptedkeys: dek already exists for organization")

// Store is the abstraction over encrypted-DEK durable storage.
type Store interface {
	// Put inserts the encrypted DEK. Returns ErrAlreadyExists if a DEK is
	// already stored for the organization.
	Put(ctx context.Context, orgID uuid.UUID, encryptedDEKB64 string) error

	// Get returns the encrypted DEK as base64, or the empty string if no DEK
	// is stored for the organization. (Empty string, not error, matches
	// today's repository contract and lets the cipher service distinguish
	// "never had one — generate now" from "lookup failed — bail out".)
	Get(ctx context.Context, orgID uuid.UUID) (string, error)

	// Delete removes the stored DEK. Idempotent: deleting a missing key is
	// not an error. Use with extreme caution — see package docs on
	// unrecoverability.
	Delete(ctx context.Context, orgID uuid.UUID) error

	// Name returns a short identifier ("postgres", "http") for admin UI
	// display and audit logs.
	Name() string
}
