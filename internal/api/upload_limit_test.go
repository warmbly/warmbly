package api

import (
	"net/http/httptest"
	"testing"
)

// The contact and workspace archive uploads cap their own bodies; every other
// route keeps the global 10 MB cap.
func TestLargeUploadRouteIsOnlyTheSelfCappedUploads(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"POST", "/v1/contacts/imports", true},
		{"POST", "/v1/contacts/import/preview", true},
		{"POST", "/v1/contacts/import/commit", true},
		{"GET", "/v1/contacts/imports", false},
		{"POST", "/v1/contacts/imports/abc/start", false},
		{"POST", "/v1/contacts", false},
		{"POST", "/v1/emails/imports", false},
		{"POST", "/v1/organization/current/import", true},
		{"POST", "/v1/organization/current/import/preflight", true},
		{"GET", "/v1/organization/current/import", false},
		{"POST", "/v1/organization/current/export", false},
	} {
		if got := largeUploadRoute(httptest.NewRequest(tc.method, tc.path, nil)); got != tc.want {
			t.Errorf("%s %s: got %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
