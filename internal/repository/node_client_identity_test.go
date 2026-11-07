package repository_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
	"github.com/warmbly/warmbly/internal/repository"
)

// Every node client presents the node's own identity next to the fleet token.
func TestNodeClientsPresentTheNodeIdentity(t *testing.T) {
	node := uuid.NewString()
	t.Setenv("WARMBLY_NODE_ID", node)
	t.Setenv("NODE_CREDENTIAL", "node-secret")

	seen := map[string]http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = r.Header.Clone()
		http.Error(w, `{"error":"stop"}`, http.StatusTeapot)
	}))
	defer srv.Close()
	ctx := context.Background()

	keys, err := encryptedkeys.NewHTTP(srv.URL, "fleet")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = keys.Get(ctx, uuid.New())
	broker, err := kms.NewBrokered(srv.URL, "fleet")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = broker.GetDecryptedKey(ctx, "c2VhbGVk")
	blobs, err := storage.NewBrokered(srv.URL, "fleet")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = blobs.Get(ctx, "bodies/x")
	sync, err := repository.NewHTTPSyncContextRepository(srv.URL, "fleet")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sync.IsOwnConversation(ctx, uuid.New(), uuid.New(), []string{"<a@b>"}, "")
	tokens, err := repository.NewHTTPBrokeredTokenClient(srv.URL, "fleet")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tokens.Token(ctx, uuid.New())

	for _, prefix := range []string{"/api/v1/internal/dek/", "/api/v1/internal/dek/decrypt", "/api/v1/internal/blobs/presign", "/api/v1/internal/sync/own-conversation", "/api/v1/internal/cloud-link/token/"} {
		var h http.Header
		for path, got := range seen {
			if path == prefix || (strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, prefix) && path != "/api/v1/internal/dek/decrypt") {
				h = got
			}
		}
		if h == nil {
			t.Fatalf("%s was never called", prefix)
		}
		if h.Get("Authorization") != "Bearer fleet" || h.Get(nodeauth.IDHeader) != node || h.Get(nodeauth.CredentialHeader) != "node-secret" {
			t.Fatalf("%s sent bearer %q id %q credential %q", prefix, h.Get("Authorization"), h.Get(nodeauth.IDHeader), h.Get(nodeauth.CredentialHeader))
		}
	}
}
