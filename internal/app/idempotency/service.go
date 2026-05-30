package idempotency

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/errx"
)

const ttl = 24 * time.Hour

type State string

const (
	StateStarted    State = "started"
	StateReplay     State = "replay"
	StateProcessing State = "processing"
	StateConflict   State = "conflict"
)

type Record struct {
	ID           uuid.UUID
	Method       string
	Path         string
	RequestHash  string
	Status       string
	StatusCode   int
	ResponseBody []byte
	ContentType  *string
}

type Service interface {
	Begin(ctx context.Context, orgID uuid.UUID, key, method, path, requestHash string) (*Record, State, *errx.Error)
	Complete(ctx context.Context, recordID uuid.UUID, statusCode int, responseBody []byte, contentType string) *errx.Error
}

type service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) Service {
	return &service{db: db}
}

func (s *service) Begin(ctx context.Context, orgID uuid.UUID, key, method, path, requestHash string) (*Record, State, *errx.Error) {
	if s == nil || s.db == nil {
		return nil, "", errx.New(errx.ServiceUnavailable, "idempotency service is not available")
	}

	_, _ = s.db.Exec(ctx, `
		DELETE FROM api_idempotency_keys
		WHERE organization_id = $1 AND key = $2 AND expires_at < now()
	`, orgID, key)

	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO api_idempotency_keys (
			organization_id, key, method, path, request_hash, status, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, 'processing', now() + ($6::integer * interval '1 second'))
		ON CONFLICT (organization_id, key) DO NOTHING
		RETURNING id
	`, orgID, key, method, path, requestHash, int(ttl.Seconds())).Scan(&id)
	if err == nil {
		return &Record{ID: id, Method: method, Path: path, RequestHash: requestHash, Status: "processing"}, StateStarted, nil
	}
	if err != pgx.ErrNoRows {
		return nil, "", errx.InternalError()
	}

	record, xerr := s.get(ctx, orgID, key)
	if xerr != nil {
		return nil, "", xerr
	}
	if record.Method != method || record.Path != path || record.RequestHash != requestHash {
		return record, StateConflict, nil
	}
	if record.Status == "completed" {
		return record, StateReplay, nil
	}
	return record, StateProcessing, nil
}

func (s *service) Complete(ctx context.Context, recordID uuid.UUID, statusCode int, responseBody []byte, contentType string) *errx.Error {
	if s == nil || s.db == nil {
		return errx.New(errx.ServiceUnavailable, "idempotency service is not available")
	}
	_, err := s.db.Exec(ctx, `
		UPDATE api_idempotency_keys
		SET status = 'completed',
		    status_code = $2,
		    response_body = $3,
		    content_type = NULLIF($4, ''),
		    updated_at = now()
		WHERE id = $1
	`, recordID, statusCode, responseBody, contentType)
	if err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) get(ctx context.Context, orgID uuid.UUID, key string) (*Record, *errx.Error) {
	var record Record
	err := s.db.QueryRow(ctx, `
		SELECT id, method, path, request_hash, status, COALESCE(status_code, 0), COALESCE(response_body, ''::bytea), content_type
		FROM api_idempotency_keys
		WHERE organization_id = $1 AND key = $2
	`, orgID, key).Scan(
		&record.ID,
		&record.Method,
		&record.Path,
		&record.RequestHash,
		&record.Status,
		&record.StatusCode,
		&record.ResponseBody,
		&record.ContentType,
	)
	if err == pgx.ErrNoRows {
		return nil, errx.ErrNotFound
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	return &record, nil
}
