package idempotency

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/errx"
)

const ttl = 24 * time.Hour

// AbandonedAfter is how long a request may stay in flight before its outcome is
// reported as unknown; it is far beyond any request the API serves.
const AbandonedAfter = 10 * time.Minute

type State string

const (
	StateStarted    State = "started"
	StateReplay     State = "replay"
	StateProcessing State = "processing"
	StateConflict   State = "conflict"
	// StateUncertain is a request that ended without a definite outcome (a
	// server error, a panic, or a process that never came back), so whether its
	// side effect happened is unknown and it is neither replayed nor re-run.
	StateUncertain State = "uncertain"
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
	// Abandoned is a processing record nobody has finished within AbandonedAfter.
	Abandoned bool
}

// Scope is what a key is unique within: a workspace, or an agency for an agency key on agency endpoints.
type Scope struct {
	OrganizationID uuid.UUID
	AgencyID       uuid.UUID
}

func OrgScope(id uuid.UUID) Scope    { return Scope{OrganizationID: id} }
func AgencyScope(id uuid.UUID) Scope { return Scope{AgencyID: id} }

// scopeSQL is the scope column, its id and the unique index a claim conflicts on; the column is one of two constants, never request input.
func scopeSQL(scope Scope) (column string, id uuid.UUID, conflict string) {
	if scope.AgencyID != uuid.Nil {
		return "agency_id", scope.AgencyID, "(agency_id, key) WHERE agency_id IS NOT NULL"
	}
	return "organization_id", scope.OrganizationID, "(organization_id, key)"
}

// Service records one logical operation per scope (a workspace or an agency) and key. Begin claims
// the key; every claim then ends in exactly one of Complete (a definite answer
// to replay), Release (refused before it ran, so the key is free again) or
// MarkUncertain (ended without a definite outcome).
type Service interface {
	Begin(ctx context.Context, scope Scope, key, method, path, requestHash string) (*Record, State, *errx.Error)
	Complete(ctx context.Context, recordID uuid.UUID, statusCode int, responseBody []byte, contentType string) *errx.Error
	Release(ctx context.Context, recordID uuid.UUID) *errx.Error
	MarkUncertain(ctx context.Context, recordID uuid.UUID, statusCode int) *errx.Error
}

type service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) Service {
	return &service{db: db}
}

func (s *service) Begin(ctx context.Context, scope Scope, key, method, path, requestHash string) (*Record, State, *errx.Error) {
	if s == nil || s.db == nil {
		return nil, "", errx.New(errx.ServiceUnavailable, "idempotency service is not available")
	}
	if (scope.OrganizationID == uuid.Nil) == (scope.AgencyID == uuid.Nil) {
		return nil, "", errx.InternalError()
	}

	// A record released between the claim and the read is claimed again.
	for attempt := 0; attempt < 2; attempt++ {
		record, state, xerr := s.begin(ctx, scope, key, method, path, requestHash)
		if xerr != nil || record != nil {
			return record, state, xerr
		}
	}
	return nil, "", errx.New(errx.Conflict, "an identical request is still processing")
}

func (s *service) begin(ctx context.Context, scope Scope, key, method, path, requestHash string) (*Record, State, *errx.Error) {
	column, scopeID, conflict := scopeSQL(scope)
	// An expired record is taken over in the same statement, so two requests
	// racing on it still produce exactly one claim.
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO api_idempotency_keys (
			`+column+`, key, method, path, request_hash, status, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, 'processing', now() + ($6::integer * interval '1 second'))
		ON CONFLICT `+conflict+` DO UPDATE
		SET id = gen_random_uuid(),
		    method = EXCLUDED.method,
		    path = EXCLUDED.path,
		    request_hash = EXCLUDED.request_hash,
		    status = 'processing',
		    status_code = NULL,
		    response_body = NULL,
		    content_type = NULL,
		    created_at = now(),
		    updated_at = now(),
		    expires_at = EXCLUDED.expires_at
		WHERE api_idempotency_keys.expires_at < now()
		RETURNING id
	`, scopeID, key, method, path, requestHash, int(ttl.Seconds())).Scan(&id)
	if err == nil {
		return &Record{ID: id, Method: method, Path: path, RequestHash: requestHash, Status: "processing"}, StateStarted, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", errx.InternalError()
	}

	record, xerr := s.get(ctx, column, scopeID, key)
	if xerr != nil || record == nil {
		return nil, "", xerr
	}
	return record, stateOf(record, method, path, requestHash), nil
}

// stateOf decides how a request answers against the record already holding its
// key. Identity is checked first: a different request never learns anything
// about the one that holds the key.
func stateOf(record *Record, method, path, requestHash string) State {
	switch {
	case record.Method != method || record.Path != path || record.RequestHash != requestHash:
		return StateConflict
	case record.Status == "completed":
		return StateReplay
	// A processing record with a status code is one MarkUncertain closed. The
	// previous release reads the same row as still processing, which is safe.
	case record.StatusCode != 0, record.Abandoned:
		return StateUncertain
	default:
		return StateProcessing
	}
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
		WHERE id = $1 AND status = 'processing' AND status_code IS NULL
	`, recordID, statusCode, responseBody, contentType)
	if err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) Release(ctx context.Context, recordID uuid.UUID) *errx.Error {
	if s == nil || s.db == nil {
		return errx.New(errx.ServiceUnavailable, "idempotency service is not available")
	}
	_, err := s.db.Exec(ctx, `
		DELETE FROM api_idempotency_keys
		WHERE id = $1 AND status = 'processing' AND status_code IS NULL
	`, recordID)
	if err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) MarkUncertain(ctx context.Context, recordID uuid.UUID, statusCode int) *errx.Error {
	if s == nil || s.db == nil {
		return errx.New(errx.ServiceUnavailable, "idempotency service is not available")
	}
	if statusCode <= 0 {
		statusCode = 500
	}
	_, err := s.db.Exec(ctx, `
		UPDATE api_idempotency_keys
		SET status_code = $2, updated_at = now()
		WHERE id = $1 AND status = 'processing' AND status_code IS NULL
	`, recordID, statusCode)
	if err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) get(ctx context.Context, column string, scopeID uuid.UUID, key string) (*Record, *errx.Error) {
	var record Record
	err := s.db.QueryRow(ctx, `
		SELECT id, method, path, request_hash, status, COALESCE(status_code, 0), COALESCE(response_body, ''::bytea), content_type,
		       status = 'processing' AND updated_at < now() - ($3::integer * interval '1 second')
		FROM api_idempotency_keys
		WHERE `+column+` = $1 AND key = $2
	`, scopeID, key, int(AbandonedAfter.Seconds())).Scan(
		&record.ID,
		&record.Method,
		&record.Path,
		&record.RequestHash,
		&record.Status,
		&record.StatusCode,
		&record.ResponseBody,
		&record.ContentType,
		&record.Abandoned,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	return &record, nil
}
