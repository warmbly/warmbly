package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// StorageBackend mirrors a row from the storage_backends table.
// Sensitive Config values must be cipher-encrypted before Upsert.
type StorageBackend struct {
	ID         uuid.UUID       `json:"id"`
	Kind       string          `json:"kind"`
	Provider   string          `json:"provider"`
	Name       string          `json:"name"`
	Config     json.RawMessage `json:"config"`
	IsActive   bool            `json:"is_active"`
	IsReadonly bool            `json:"is_readonly"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type StorageBackendRepository interface {
	List(ctx context.Context) ([]StorageBackend, error)
	ListByKind(ctx context.Context, kind string) ([]StorageBackend, error)
	GetActive(ctx context.Context, kind string) (*StorageBackend, error)
	GetByKindProvider(ctx context.Context, kind, provider string) (*StorageBackend, error)
	Create(ctx context.Context, b *StorageBackend) error
	UpdateConfig(ctx context.Context, id uuid.UUID, name string, config json.RawMessage, isReadonly bool) error
	SetActive(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type storageBackendRepository struct {
	db *db.DB
}

func NewStorageBackendRepository(d *db.DB) StorageBackendRepository {
	return &storageBackendRepository{db: d}
}

const storageBackendColumns = `id, kind, provider, name, config, is_active, is_readonly, created_at, updated_at`

func scanStorageBackend(row pgx.Row) (*StorageBackend, error) {
	var b StorageBackend
	if err := row.Scan(&b.ID, &b.Kind, &b.Provider, &b.Name, &b.Config, &b.IsActive, &b.IsReadonly, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *storageBackendRepository) List(ctx context.Context) ([]StorageBackend, error) {
	rows, err := r.db.Query(ctx, `SELECT `+storageBackendColumns+` FROM storage_backends ORDER BY kind, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StorageBackend
	for rows.Next() {
		b, err := scanStorageBackend(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *storageBackendRepository) ListByKind(ctx context.Context, kind string) ([]StorageBackend, error) {
	rows, err := r.db.Query(ctx, `SELECT `+storageBackendColumns+` FROM storage_backends WHERE kind = $1 ORDER BY created_at DESC`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StorageBackend
	for rows.Next() {
		b, err := scanStorageBackend(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *storageBackendRepository) GetActive(ctx context.Context, kind string) (*StorageBackend, error) {
	row := r.db.QueryRow(ctx, `SELECT `+storageBackendColumns+` FROM storage_backends WHERE kind = $1 AND is_active`, kind)
	b, err := scanStorageBackend(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

func (r *storageBackendRepository) GetByKindProvider(ctx context.Context, kind, provider string) (*StorageBackend, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+storageBackendColumns+` FROM storage_backends WHERE kind = $1 AND provider = $2 LIMIT 1`,
		kind, provider)
	b, err := scanStorageBackend(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

func (r *storageBackendRepository) Create(ctx context.Context, b *StorageBackend) error {
	if len(b.Config) == 0 {
		b.Config = json.RawMessage("{}")
	}
	const q = `
		INSERT INTO storage_backends (kind, provider, name, config, is_active, is_readonly)
		VALUES ($1, $2, $3, $4, FALSE, $5)
		RETURNING id, is_active, created_at, updated_at
	`
	err := r.db.QueryRow(ctx, q, b.Kind, b.Provider, b.Name, []byte(b.Config), b.IsReadonly).
		Scan(&b.ID, &b.IsActive, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return fmt.Errorf("storage_backends create: %w", err)
	}
	return nil
}

func (r *storageBackendRepository) UpdateConfig(ctx context.Context, id uuid.UUID, name string, config json.RawMessage, isReadonly bool) error {
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	const q = `
		UPDATE storage_backends
		SET name = $2, config = $3, is_readonly = $4, updated_at = now()
		WHERE id = $1
	`
	tag, err := r.db.Exec(ctx, q, id, name, []byte(config), isReadonly)
	if err != nil {
		return fmt.Errorf("storage_backends update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("storage_backends update: id %s not found", id)
	}
	return nil
}

// SetActive atomically flips the chosen row to active and deactivates any
// existing active row for the same kind. The partial unique index requires
// the deactivation to land first, so this runs in a single transaction.
func (r *storageBackendRepository) SetActive(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var kind string
	if err := tx.QueryRow(ctx, `SELECT kind FROM storage_backends WHERE id = $1`, id).Scan(&kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("storage_backends: id %s not found", id)
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE storage_backends SET is_active = FALSE, updated_at = now() WHERE kind = $1 AND is_active`, kind); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE storage_backends SET is_active = TRUE, updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *storageBackendRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM storage_backends WHERE id = $1`, id)
	return err
}
