package nodeagent

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
)

func TestHeartbeatPresentsTheNodeIdentity(t *testing.T) {
	for _, credential := range []string{"node-secret", ""} {
		var got http.Header
		client := &http.Client{Transport: heartbeatTransport(func(r *http.Request) (*http.Response, error) {
			got = r.Header.Clone()
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
		})}
		id := uuid.New()
		a := New(Config{NodeID: id, Role: models.NodeRoleConsumer, BaseURL: "http://cp", Token: "fleet", Credential: credential, HTTPClient: client})
		a.beat(context.Background(), false, true)
		if got.Get("Authorization") != "Bearer fleet" {
			t.Fatalf("bearer %q", got.Get("Authorization"))
		}
		wantID := ""
		if credential != "" {
			wantID = id.String()
		}
		if got.Get(nodeauth.IDHeader) != wantID || got.Get(nodeauth.CredentialHeader) != credential {
			t.Fatalf("sent id %q credential %q, want %q %q", got.Get(nodeauth.IDHeader), got.Get(nodeauth.CredentialHeader), wantID, credential)
		}
	}
}
