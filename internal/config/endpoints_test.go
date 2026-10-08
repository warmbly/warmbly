package config

import "testing"

func TestNormalizeWebsocketURLWithPathsAndQueries(t *testing.T) {
	for input, want := range map[string]string{
		"https://realtime.test/socket?tenant=old":                  "wss://realtime.test/socket/websocket?tenant=old",
		"http://localhost:4000/proxy/socket/websocket/?tenant=old": "ws://localhost:4000/proxy/socket/websocket?tenant=old",
		"wss://realtime.test?tenant=old":                           "wss://realtime.test/socket/websocket?tenant=old",
		"file:///etc/passwd":                                       "", "wss://user:password@realtime.test": "", "://bad": "",
	} {
		if got := NormalizeWebsocketURL(input); got != want {
			t.Errorf("NormalizeWebsocketURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeWebsocketURLPreservesLegacyConfiguration(t *testing.T) {
	for _, input := range []string{"", " wss://realtime.test/ ", "wss://realtime.test/socket", "wss://realtime.test/socket/websocket/"} {
		want := "wss://realtime.test/socket/websocket"
		if input == "" {
			want = ""
		}
		t.Setenv("WEBSOCKET_URL", input)
		if got := NormalizeWebsocketURL(input); got != want {
			t.Errorf("resolved websocket %q became %q, want %q", input, got, want)
		}
		if got := WebsocketURL(); got != want {
			t.Errorf("environment websocket %q became %q, want %q", input, got, want)
		}
	}
}

// The hosted form URL has to be reachable on every install shape: the shared
// host keeps its port (a share link that drops it points at nothing), and the
// scheme follows the host rather than the port, because an install can
// terminate TLS on any port and a ported https deployment must not be handed
// http:// links (PR #368).
func TestFormURLsFollowTheInstallHost(t *testing.T) {
	for _, tc := range []struct {
		formsDomain string
		wantBase    string
		wantShare   string
		wantCNAME   string
	}{
		{"localhost:8090", "http://localhost:8090", "http://localhost:8090/f/abc", "localhost"},
		{"127.0.0.1:8090", "http://127.0.0.1:8090", "http://127.0.0.1:8090/f/abc", "127.0.0.1"},
		{"192.168.1.5:8090", "http://192.168.1.5:8090", "http://192.168.1.5:8090/f/abc", "192.168.1.5"},
		{"forms.example.com", "https://forms.example.com", "https://forms.example.com/f/abc", "forms.example.com"},
		{"forms.example.com:8443", "https://forms.example.com:8443", "https://forms.example.com:8443/f/abc", "forms.example.com"},
		{"https://Forms.Example.com/", "https://forms.example.com", "https://forms.example.com/f/abc", "forms.example.com"},
	} {
		t.Run(tc.formsDomain, func(t *testing.T) {
			t.Setenv("FORMS_DOMAIN", tc.formsDomain)
			if got := FormsBaseURL(); got != tc.wantBase {
				t.Errorf("FormsBaseURL() = %q, want %q", got, tc.wantBase)
			}
			if got := GetFormURL("abc"); got != tc.wantShare {
				t.Errorf("GetFormURL() = %q, want %q", got, tc.wantShare)
			}
			// What the handler stamps on every form: the shared host, resolved
			// through FormsHost, then built into a URL.
			if got := FormURLOn(FormsURLHost(), "abc"); got != tc.wantShare {
				t.Errorf("FormURLOn(FormsURLHost()) = %q, want %q", got, tc.wantShare)
			}
			// The CNAME target is a DNS name, so it never carries the port.
			if got := FormsHostname(); got != tc.wantCNAME {
				t.Errorf("FormsHostname() = %q, want %q", got, tc.wantCNAME)
			}
		})
	}
}

// A verified custom forms domain replaces the shared host and is always a bare
// name, so its links stay https whatever the install runs on.
func TestFormURLOnCustomDomain(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "localhost:8090")
	if got := FormURLOn("forms.acme.com", "abc"); got != "https://forms.acme.com/f/abc" {
		t.Errorf("custom domain URL = %q", got)
	}
	// No host and no configured base is an empty URL, never a relative one.
	t.Setenv("FORMS_DOMAIN", "")
	if got := FormURLOn("", "abc"); got != "" {
		t.Errorf("unconfigured install URL = %q, want empty", got)
	}
}
