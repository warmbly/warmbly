package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
)

func init() { gin.SetMode(gin.TestMode) }

type mockEKStore struct {
	put    func(ctx context.Context, orgID uuid.UUID, dek string) error
	get    func(ctx context.Context, orgID uuid.UUID) (string, error)
	delete func(ctx context.Context, orgID uuid.UUID) error
}

func (m *mockEKStore) Put(ctx context.Context, orgID uuid.UUID, dek string) error {
	return m.put(ctx, orgID, dek)
}
func (m *mockEKStore) Get(ctx context.Context, orgID uuid.UUID) (string, error) {
	return m.get(ctx, orgID)
}
func (m *mockEKStore) Delete(ctx context.Context, orgID uuid.UUID) error {
	return m.delete(ctx, orgID)
}
func (m *mockEKStore) Name() string { return "mock" }

func newDEKRouter(t *testing.T, store encryptedkeys.Store) *gin.Engine {
	t.Helper()
	h := &Handler{EncryptedKeys: store}
	r := gin.New()
	r.GET("/dek/:orgID", h.InternalGetDEK)
	r.PUT("/dek/:orgID", h.InternalPutDEK)
	r.DELETE("/dek/:orgID", h.InternalDeleteDEK)
	return r
}

func TestInternalGetDEK_Found(t *testing.T) {
	id := uuid.New()
	store := &mockEKStore{
		get: func(_ context.Context, u uuid.UUID) (string, error) {
			if u != id {
				t.Fatalf("wrong userID: got %s want %s", u, id)
			}
			return "encrypted-blob", nil
		},
	}
	r := newDEKRouter(t, store)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/dek/"+id.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var p dekPayload
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.EncryptedDataKey != "encrypted-blob" {
		t.Fatalf("payload mismatch: %q", p.EncryptedDataKey)
	}
}

func TestInternalGetDEK_NotFoundReturns404(t *testing.T) {
	store := &mockEKStore{
		get: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "", nil // empty = not found per Store contract
		},
	}
	r := newDEKRouter(t, store)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/dek/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestInternalGetDEK_BadUUID(t *testing.T) {
	r := newDEKRouter(t, &mockEKStore{})
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/dek/not-a-uuid", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestInternalGetDEK_StoreError(t *testing.T) {
	store := &mockEKStore{
		get: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "", errors.New("kaboom")
		},
	}
	r := newDEKRouter(t, store)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/dek/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestInternalPutDEK_Created(t *testing.T) {
	id := uuid.New()
	store := &mockEKStore{
		put: func(_ context.Context, u uuid.UUID, dek string) error {
			if u != id {
				t.Fatalf("userID mismatch")
			}
			if dek != "blob" {
				t.Fatalf("dek mismatch: %q", dek)
			}
			return nil
		},
	}
	r := newDEKRouter(t, store)
	body, _ := json.Marshal(dekPayload{EncryptedDataKey: "blob"})
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/dek/"+id.String(), bytes.NewReader(body))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestInternalPutDEK_ConflictReturns409(t *testing.T) {
	store := &mockEKStore{
		put: func(_ context.Context, _ uuid.UUID, _ string) error {
			return encryptedkeys.ErrAlreadyExists
		},
	}
	r := newDEKRouter(t, store)
	body, _ := json.Marshal(dekPayload{EncryptedDataKey: "blob"})
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/dek/"+uuid.New().String(), bytes.NewReader(body))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestInternalPutDEK_RejectsEmptyBody(t *testing.T) {
	r := newDEKRouter(t, &mockEKStore{})
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/dek/"+uuid.New().String(), strings.NewReader(`{}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty key, got %d", w.Code)
	}
}

func TestInternalDeleteDEK_NoContent(t *testing.T) {
	store := &mockEKStore{
		delete: func(_ context.Context, _ uuid.UUID) error { return nil },
	}
	r := newDEKRouter(t, store)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/dek/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
