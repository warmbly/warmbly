package partner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

const (
	idempotencyTTL = 24 * time.Hour
	// A claim nobody finished within this long belongs to a request that died; the key may run again.
	idempotencyAbandoned = 2 * time.Minute
)

var (
	ErrIdempotencyRequired = errx.NewWithIdentifier(errx.BadRequest, "idempotency_key_required", "Idempotency-Key is required on every write.")
	ErrIdempotencyInvalid  = errx.NewWithIdentifier(errx.BadRequest, "invalid_request", "Idempotency-Key is 1-255 visible ASCII characters.")
	ErrIdempotencyConflict = errx.NewWithIdentifier(errx.Conflict, "idempotency_conflict", "This Idempotency-Key was used with a different request.")
	ErrIdempotencyBusy     = &errx.Error{Code: errx.Conflict, Identifier: "idempotency_in_progress", Message: "A request with this Idempotency-Key is still running.", RetryAfter: 2}
)

// Replay is a stored answer to send again.
type Replay struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
}

// ValidIdempotencyKey is 1-255 visible ASCII characters.
func ValidIdempotencyKey(key string) bool {
	if key == "" || len(key) > 255 {
		return false
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

// RequestHash identifies a request by method, path and body.
func RequestHash(method, path string, body []byte) string {
	h := sha256.New()
	for _, part := range [][]byte{[]byte(method), []byte(path), body} {
		h.Write(part)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Claim is the hold one request has on an Idempotency-Key; only its holder may complete or release it.
type Claim struct {
	Key   string
	Token uuid.UUID
}

// BeginIdempotent claims key for this request, or returns the replay of the answer it already gave the same request.
func (s *Service) BeginIdempotent(ctx context.Context, c *Caller, key, hash string) (*Claim, *Replay, *errx.Error) {
	token, prev, err := s.Repo.ClaimIdempotency(ctx, c.Partner.ID, c.Mode, key, hash, idempotencyAbandoned)
	if err != nil {
		return nil, nil, internal(err)
	}
	if prev == nil {
		return &Claim{Key: key, Token: token}, nil, nil
	}
	if prev.RequestHash != hash {
		return nil, nil, ErrIdempotencyConflict
	}
	if prev.Completed {
		r, xerr := s.openReplay(prev.Sealed, prev.StatusCode)
		return nil, r, xerr
	}
	return nil, nil, ErrIdempotencyBusy
}

func (s *Service) openReplay(sealed string, status int) (*Replay, *errx.Error) {
	if s.Sealer == nil || sealed == "" {
		return nil, internal(errIdempotencyUnsealed)
	}
	plain, err := s.Sealer.Decrypt(sealed)
	if err != nil {
		return nil, internal(err)
	}
	var r Replay
	if err := json.Unmarshal([]byte(plain), &r); err != nil {
		return nil, internal(err)
	}
	if r.Status == 0 {
		r.Status = status
	}
	return &r, nil
}

var errIdempotencyUnsealed = errx.New(errx.Internal, "stored partner answer cannot be opened")

// FinishIdempotent stores a definite answer, sealed because some carry a secret. A server error or a
// refusal before the request ran frees the key, so a retry with it runs again. Either acts only while
// the claim is still this request's.
func (s *Service) FinishIdempotent(ctx context.Context, c *Caller, claim *Claim, status int, contentType string, body []byte) {
	if claim == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if status >= 500 || status == 401 || status == 403 || status == 429 || s.Sealer == nil {
		if err := s.Repo.ReleaseIdempotency(ctx, c.Partner.ID, c.Mode, claim.Key, claim.Token); err != nil {
			errs.CaptureException(err)
		}
		return
	}
	raw, err := json.Marshal(Replay{Status: status, ContentType: contentType, Body: body})
	if err == nil {
		var sealed string
		if sealed, err = s.Sealer.Encrypt(string(raw)); err == nil {
			err = s.Repo.CompleteIdempotency(ctx, c.Partner.ID, c.Mode, claim.Key, claim.Token, status, sealed)
		}
	}
	if err != nil {
		errs.CaptureException(err)
		_ = s.Repo.ReleaseIdempotency(ctx, c.Partner.ID, c.Mode, claim.Key, claim.Token)
	}
}
