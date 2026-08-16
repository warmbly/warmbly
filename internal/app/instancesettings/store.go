package instancesettings

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store reads and writes the singleton settings row.
type Store interface {
	Get(ctx context.Context) (Document, error)
	Put(ctx context.Context, doc Document, updatedBy *uuid.UUID) error
}

type pgStore struct {
	db *pgxpool.Pool
}

// NewStore builds the Postgres-backed store.
func NewStore(db *pgxpool.Pool) Store { return &pgStore{db: db} }

func (s *pgStore) Get(ctx context.Context) (Document, error) {
	doc := Defaults()

	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT doc FROM instance_settings WHERE id = true`).Scan(&raw)
	if err == pgx.ErrNoRows {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	// An empty or partial document keeps the defaults for the keys it omits.
	if len(raw) > 0 {
		if uerr := json.Unmarshal(raw, &doc); uerr != nil {
			return Defaults(), uerr
		}
	}
	doc.Normalize()
	return doc, nil
}

func (s *pgStore) Put(ctx context.Context, doc Document, updatedBy *uuid.UUID) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO instance_settings (id, doc, updated_by, updated_at)
		VALUES (true, $1::jsonb, $2, NOW())
		ON CONFLICT (id) DO UPDATE
		SET doc = EXCLUDED.doc, updated_by = EXCLUDED.updated_by, updated_at = NOW()
	`, raw, updatedBy)
	return err
}
