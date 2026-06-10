package encryptedkeys

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// PostgresStore stores encrypted DEKs in the organization_encrypted_keys
// table. Only the backend uses this impl directly; workers reach DEKs via the
// HTTP store (which proxies through the backend).
type PostgresStore struct {
	db *db.DB
}

func NewPostgres(d *db.DB) *PostgresStore {
	return &PostgresStore{db: d}
}

func (s *PostgresStore) Name() string { return "postgres" }

func (s *PostgresStore) Put(ctx context.Context, orgID uuid.UUID, encryptedDEKB64 string) error {
	const q = `
		INSERT INTO organization_encrypted_keys (organization_id, encrypted_data_key)
		VALUES ($1, $2)
	`
	_, err := s.db.Exec(ctx, q, orgID, encryptedDEKB64)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return ErrAlreadyExists
		}
		return fmt.Errorf("encryptedkeys.postgres: put: %w", err)
	}
	return nil
}

func (s *PostgresStore) Get(ctx context.Context, orgID uuid.UUID) (string, error) {
	const q = `SELECT encrypted_data_key FROM organization_encrypted_keys WHERE organization_id = $1`
	var out string
	err := s.db.QueryRow(ctx, q, orgID).Scan(&out)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("encryptedkeys.postgres: get: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) Delete(ctx context.Context, orgID uuid.UUID) error {
	const q = `DELETE FROM organization_encrypted_keys WHERE organization_id = $1`
	_, err := s.db.Exec(ctx, q, orgID)
	if err != nil {
		return fmt.Errorf("encryptedkeys.postgres: delete: %w", err)
	}
	return nil
}
