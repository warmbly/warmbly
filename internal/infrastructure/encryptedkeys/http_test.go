package encryptedkeys

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const testToken = "test-bearer-token"

type fakeBackend struct {
	t         *testing.T
	keys      map[string]string
	gotAuth   string
	failPut   bool
	failGet   bool
	getStatus int
	putStatus int
	delStatus int
}

func (f *fakeBackend) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/internal/dek/", func(w http.ResponseWriter, r *http.Request) {
		f.gotAuth = r.Header.Get("Authorization")
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/internal/dek/")
		if _, err := uuid.Parse(id); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if f.getStatus != 0 {
				w.WriteHeader(f.getStatus)
				return
			}
			v, ok := f.keys[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(dekPayload{EncryptedDataKey: v})
		case http.MethodPut:
			if f.putStatus != 0 {
				w.WriteHeader(f.putStatus)
				return
			}
			body, _ := io.ReadAll(r.Body)
			var p dekPayload
			if err := json.Unmarshal(body, &p); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if _, exists := f.keys[id]; exists {
				w.WriteHeader(http.StatusConflict)
				return
			}
			f.keys[id] = p.EncryptedDataKey
			w.WriteHeader(http.StatusCreated)
		case http.MethodDelete:
			if f.delStatus != 0 {
				w.WriteHeader(f.delStatus)
				return
			}
			delete(f.keys, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func newHTTPStoreAgainst(t *testing.T, h http.Handler) (*HTTPStore, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s, err := NewHTTP(srv.URL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	return s, srv
}

func TestHTTPStore_RoundTrip(t *testing.T) {
	be := &fakeBackend{t: t, keys: map[string]string{}}
	s, _ := newHTTPStoreAgainst(t, be.handler())
	ctx := context.Background()
	uid := uuid.New()

	got, err := s.Get(ctx, uid)
	if err != nil || got != "" {
		t.Fatalf("Get on empty store: got=%q err=%v", got, err)
	}

	if err := s.Put(ctx, uid, "ciphertext-1"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err = s.Get(ctx, uid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "ciphertext-1" {
		t.Fatalf("Get: got %q want %q", got, "ciphertext-1")
	}

	if err := s.Delete(ctx, uid); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err = s.Get(ctx, uid)
	if err != nil || got != "" {
		t.Fatalf("Get after delete: got=%q err=%v", got, err)
	}
}

func TestHTTPStore_PutConflictReturnsErrAlreadyExists(t *testing.T) {
	be := &fakeBackend{t: t, keys: map[string]string{}}
	s, _ := newHTTPStoreAgainst(t, be.handler())
	ctx := context.Background()
	uid := uuid.New()

	if err := s.Put(ctx, uid, "x"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, uid, "y"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Put: got %v want ErrAlreadyExists", err)
	}
}

func TestHTTPStore_SendsBearerAuth(t *testing.T) {
	be := &fakeBackend{t: t, keys: map[string]string{}}
	s, _ := newHTTPStoreAgainst(t, be.handler())
	_, _ = s.Get(context.Background(), uuid.New())
	if be.gotAuth != "Bearer "+testToken {
		t.Fatalf("auth header: got %q want %q", be.gotAuth, "Bearer "+testToken)
	}
}

func TestHTTPStore_DeleteOnMissingIsOK(t *testing.T) {
	be := &fakeBackend{t: t, keys: map[string]string{}}
	s, _ := newHTTPStoreAgainst(t, be.handler())
	if err := s.Delete(context.Background(), uuid.New()); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestHTTPStore_BackendErrorBubblesUp(t *testing.T) {
	be := &fakeBackend{t: t, keys: map[string]string{}, getStatus: http.StatusInternalServerError}
	s, _ := newHTTPStoreAgainst(t, be.handler())
	if _, err := s.Get(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestNewHTTP_Validation(t *testing.T) {
	if _, err := NewHTTP("", "t"); err == nil {
		t.Fatal("expected error on empty URL")
	}
	if _, err := NewHTTP("http://x", ""); err == nil {
		t.Fatal("expected error on empty token")
	}
}

func TestHTTPStore_Name(t *testing.T) {
	s, err := NewHTTP("http://x", "t")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "http" {
		t.Fatalf("name: got %q want %q", s.Name(), "http")
	}
}
