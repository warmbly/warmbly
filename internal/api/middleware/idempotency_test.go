package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/idempotency"
	"github.com/warmbly/warmbly/internal/errx"
)

type fakeIdempotencyService struct {
	record *idempotency.Record
	state  idempotency.State
	body   []byte
}

func (s *fakeIdempotencyService) Begin(ctx context.Context, orgID uuid.UUID, key, method, path, requestHash string) (*idempotency.Record, idempotency.State, *errx.Error) {
	if s.record == nil {
		s.record = &idempotency.Record{ID: uuid.New(), StatusCode: http.StatusCreated}
	}
	return s.record, s.state, nil
}

func (s *fakeIdempotencyService) Complete(ctx context.Context, recordID uuid.UUID, statusCode int, responseBody []byte, contentType string) *errx.Error {
	s.record.StatusCode = statusCode
	s.body = append([]byte(nil), responseBody...)
	return nil
}

func TestIdempotencyMiddlewareStoresResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orgID := uuid.New()
	svc := &fakeIdempotencyService{state: idempotency.StateStarted}
	h := &Handler{IdempotencyService: svc}

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.POST("/contacts",
		func(c *gin.Context) {
			c.Set(OrganizationIDKey, orgID)
			c.Next()
		},
		h.IdempotencyMiddleware(),
		func(c *gin.Context) {
			c.JSON(http.StatusCreated, gin.H{"id": "contact_123"})
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/contacts", nil)
	req.Header.Set(IdempotencyKeyHeader, "idem_123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if string(svc.body) == "" {
		t.Fatal("expected response body to be stored")
	}
}

func TestIdempotencyMiddlewareReplaysResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orgID := uuid.New()
	contentType := "application/json; charset=utf-8"
	svc := &fakeIdempotencyService{
		state: idempotency.StateReplay,
		record: &idempotency.Record{
			ID:           uuid.New(),
			StatusCode:   http.StatusCreated,
			ResponseBody: []byte(`{"id":"contact_123"}`),
			ContentType:  &contentType,
		},
	}
	h := &Handler{IdempotencyService: svc}

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.POST("/contacts",
		func(c *gin.Context) {
			c.Set(OrganizationIDKey, orgID)
			c.Next()
		},
		h.IdempotencyMiddleware(),
		func(c *gin.Context) {
			c.JSON(http.StatusTeapot, gin.H{"unexpected": true})
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/contacts", nil)
	req.Header.Set(IdempotencyKeyHeader, "idem_123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Header().Get(IdempotencyReplayedHeader) != "true" {
		t.Fatalf("missing replay header")
	}
	if got := rec.Body.String(); got != `{"id":"contact_123"}` {
		t.Fatalf("body = %q", got)
	}
}
