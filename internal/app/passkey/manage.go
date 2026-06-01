package passkey

import (
	"context"
	"encoding/base64"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// CredentialView is the safe, display-only projection of a stored passkey.
// CredentialID is base64url so the client can pass it to the WebAuthn signal
// API (to prune deleted passkeys from the provider's picker).
type CredentialView struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Provider     string     `json:"provider,omitempty"`
	CredentialID string     `json:"credential_id"`
	Transports   []string   `json:"transports"`
	BackupState  bool       `json:"backup_state"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

func (s *service) ListCredentials(ctx context.Context, userID uuid.UUID) ([]*CredentialView, *errx.Error) {
	creds, xerr := s.repo.ListByUser(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	views := make([]*CredentialView, 0, len(creds))
	for _, c := range creds {
		views = append(views, toView(c))
	}

	return views, nil
}

func (s *service) RenameCredential(ctx context.Context, userID, id uuid.UUID, name string) (*CredentialView, *errx.Error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxPasskeyName {
		return nil, errx.ErrPasskeyName
	}

	if xerr := s.repo.Rename(ctx, userID, id, name); xerr != nil {
		return nil, xerr
	}

	// Return the updated view so the client can reconcile without guessing.
	creds, xerr := s.repo.ListByUser(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}
	for _, c := range creds {
		if c.ID == id {
			return toView(c), nil
		}
	}

	return nil, errx.ErrPasskeyNotFound
}

func (s *service) DeleteCredential(ctx context.Context, userID, id uuid.UUID) *errx.Error {
	return s.repo.Delete(ctx, userID, id)
}

func toView(c *models.WebAuthnCredential) *CredentialView {
	transports := c.Transports
	if transports == nil {
		transports = []string{}
	}

	return &CredentialView{
		ID:           c.ID,
		Name:         c.Name,
		Provider:     providerName(c.AAGUID),
		CredentialID: base64.RawURLEncoding.EncodeToString(c.CredentialID),
		Transports:   transports,
		BackupState:  c.BackupState,
		CreatedAt:    c.CreatedAt,
		LastUsedAt:   c.LastUsedAt,
	}
}
