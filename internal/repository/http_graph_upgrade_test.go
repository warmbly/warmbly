package repository

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestProviderMessagesCarriesRFCIdentityAndAcceptsLegacyRows(t *testing.T) {
	for _, messageID := range []string{"<stored@fake.test>", ""} {
		t.Run(messageID, func(t *testing.T) {
			user, mailbox, id := uuid.New(), uuid.New(), uuid.New()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("user_id") != user.String() || r.URL.Query().Get("email_id") != mailbox.String() {
					t.Error("provider enumeration lost mailbox scope")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"messages": []ProviderFolderMessage{{ID: id, ProviderID: "regular", MessageID: messageID}}})
			}))
			t.Cleanup(srv.Close)
			repo := &httpSyncContextRepository{baseURL: srv.URL, client: srv.Client()}
			rows, err := repo.ListProviderMessages(t.Context(), user, mailbox, nil, 10)
			if err != nil || len(rows) != 1 || rows[0].MessageID != messageID {
				t.Fatalf("rows=%v err=%v", rows, err)
			}
		})
	}
}

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
