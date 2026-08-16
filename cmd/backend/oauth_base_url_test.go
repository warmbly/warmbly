package main

import "testing"

func TestOAuthPublicBaseURLPrefersAPIPublicURL(t *testing.T) {
	t.Setenv("API_PUBLIC_URL", "https://api.example.com/")

	if got := oauthPublicBaseURL("0.0.0.0:8080"); got != "https://api.example.com" {
		t.Fatalf("got %q, want the trailing slash trimmed public URL", got)
	}
}

// A stock local install sets no API_PUBLIC_URL. The redirect_uri still has to
// be an absolute URL a browser can open, which "0.0.0.0:8080" is not.
func TestOAuthPublicBaseURLFallsBackToBrowsableAddress(t *testing.T) {
	t.Setenv("API_PUBLIC_URL", "")

	tests := []struct {
		bind string
		want string
	}{
		{"0.0.0.0:8080", "http://localhost:8080"},
		{"[::]:8080", "http://localhost:8080"},
		{":8080", "http://localhost:8080"},
		{"127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"api.internal:9000", "http://api.internal:9000"},
		// Someone who already put a real base in API_HOST keeps it.
		{"https://api.example.com/", "https://api.example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := oauthPublicBaseURL(tt.bind); got != tt.want {
			t.Errorf("oauthPublicBaseURL(%q) = %q, want %q", tt.bind, got, tt.want)
		}
	}
}

// The callback routes are registered at the root, not under /v1, so the base
// must be joined with no API prefix.
func TestOAuthPublicBaseURLBuildsRegisteredCallbackPath(t *testing.T) {
	t.Setenv("API_PUBLIC_URL", "https://api.example.com")

	got := oauthPublicBaseURL("0.0.0.0:8080") + "/addresses/google/callback"
	if want := "https://api.example.com/addresses/google/callback"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
