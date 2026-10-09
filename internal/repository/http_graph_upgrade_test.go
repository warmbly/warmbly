package repository

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestProviderMessagesOldBackendIsTypedUnsupported(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusServiceUnavailable, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		repo := &httpSyncContextRepository{baseURL: srv.URL, client: srv.Client()}
		_, err := repo.ListProviderMessages(t.Context(), uuid.New(), uuid.New(), nil, 10)
		if errors.Is(err, ErrSyncContextUnsupported) != (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
			t.Errorf("status=%d err=%v", status, err)
		}
		srv.Close()
	}
}
