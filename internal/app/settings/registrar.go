// Package settings registers env-var-driven infrastructure choices into the
// storage_backends table so the admin UI can display what's currently active.
//
// The pattern: at boot, the backend already chose its KMS / EncryptedKeys /
// Blob / EventBus / Cache adapters via FromEnv constructors. This package
// reflects those choices into the database so admin pages can show:
//
//   - which backend is active
//   - whether it's read-only (env-var driven, can't be changed at runtime)
//   - non-sensitive parts of its config (read-only display)
//
// It does NOT change the running configuration — that still comes from env
// vars. Future work (admin UI write paths for things like BlobStore + EventBus)
// will start writing to this table and triggering reconfiguration.
package settings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/warmbly/warmbly/internal/repository"
)

// Backend describes one running infrastructure adapter.
type Backend struct {
	Kind     string         // "kms" | "encrypted_keys" | "blob" | "eventbus" | "cache"
	Provider string         // adapter Name() — e.g. "local", "aws-kms", "postgres", "nats"
	Display  string         // operator-friendly label
	Config   map[string]any // safe-to-display config; never include secrets
	ReadOnly bool           // env-var driven (true) vs admin-mutable (false)
}

// Registrar reflects boot-time backend choices into storage_backends.
type Registrar struct {
	repo repository.StorageBackendRepository
}

func NewRegistrar(repo repository.StorageBackendRepository) *Registrar {
	return &Registrar{repo: repo}
}

// Register inserts or updates a row for the given backend and makes it the
// active one for its kind. Idempotent — safe to call on every boot.
func (r *Registrar) Register(ctx context.Context, b Backend) error {
	cfg, err := json.Marshal(b.Config)
	if err != nil {
		return fmt.Errorf("settings: marshal config: %w", err)
	}

	existing, err := r.repo.GetByKindProvider(ctx, b.Kind, b.Provider)
	if err != nil {
		return fmt.Errorf("settings: lookup existing: %w", err)
	}

	if existing == nil {
		row := &repository.StorageBackend{
			Kind:       b.Kind,
			Provider:   b.Provider,
			Name:       b.Display,
			Config:     cfg,
			IsReadonly: b.ReadOnly,
		}
		if err := r.repo.Create(ctx, row); err != nil {
			return fmt.Errorf("settings: create: %w", err)
		}
		return r.repo.SetActive(ctx, row.ID)
	}

	if err := r.repo.UpdateConfig(ctx, existing.ID, b.Display, cfg, b.ReadOnly); err != nil {
		return fmt.Errorf("settings: update: %w", err)
	}
	if !existing.IsActive {
		return r.repo.SetActive(ctx, existing.ID)
	}
	return nil
}

// RegisterAll is a convenience wrapper that registers a batch and returns the
// first error.
func (r *Registrar) RegisterAll(ctx context.Context, backends []Backend) error {
	for _, b := range backends {
		if err := r.Register(ctx, b); err != nil {
			return err
		}
	}
	return nil
}
