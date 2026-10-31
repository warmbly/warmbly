package nodeauth

import (
	"net/http/httptest"
	"testing"
)

func TestAuthorizeSendsTheNodeIdentityWhenHeld(t *testing.T) {
	for _, tc := range []struct {
		name, nodeID, workerID, credential, wantID string
	}{
		{"join identity", "node-a", "node-a", "secret", "node-a"},
		{"WARMBLY_NODE_ID wins over WORKER_ID", "node-a", "node-b", "secret", "node-a"},
		{"a worker named only by WORKER_ID", "", "node-b", "secret", "node-b"},
		{"no credential", "node-a", "", "", ""},
		{"no id", "", "", "secret", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WARMBLY_NODE_ID", tc.nodeID)
			t.Setenv("WORKER_ID", tc.workerID)
			t.Setenv("NODE_CREDENTIAL", tc.credential)
			req := httptest.NewRequest("GET", "/", nil)
			Authorize(req, "fleet")
			if got := req.Header.Get("Authorization"); got != "Bearer fleet" {
				t.Fatalf("bearer %q", got)
			}
			wantCred := ""
			if tc.wantID != "" {
				wantCred = tc.credential
			}
			if req.Header.Get(IDHeader) != tc.wantID || req.Header.Get(CredentialHeader) != wantCred {
				t.Fatalf("sent id %q credential %q, want %q %q", req.Header.Get(IDHeader), req.Header.Get(CredentialHeader), tc.wantID, wantCred)
			}
		})
	}
}
