package config

import "testing"

func TestTrackingConsumerScopeUsesProcessOverridesNotBackendDefaults(t *testing.T) {
	t.Setenv("KAFKA_TRACKING_TOPIC", "custom-tracking-events")
	t.Setenv("KAFKA_CONSUMER_GROUP", "custom-tracking-consumer")
	cfg := &Config{}
	consumer, err := cfg.LoadTrackingConsumerConfig(t.Context())
	if err != nil || consumer.Topic != "custom-tracking-events" || consumer.GroupID != "custom-tracking-consumer" {
		t.Fatalf("consumer scope = %+v, error = %v", consumer, err)
	}
	t.Setenv("KAFKA_TRACKING_TOPIC", "")
	t.Setenv("KAFKA_CONSUMER_GROUP", "")
	backend, err := cfg.LoadTrackingConsumerConfig(t.Context())
	if err != nil || backend.Topic != "tracking-events" || backend.GroupID != "tracking-consumer" {
		t.Fatalf("default scope = %+v, error = %v", backend, err)
	}
	if consumer.Topic == backend.Topic || consumer.GroupID == backend.GroupID {
		t.Fatal("consumer process overrides were mistaken for backend defaults")
	}
}

func TestNormalizeTrackingHost(t *testing.T) {
	cases := map[string]string{
		"t.acme.com":                   "t.acme.com",
		"  T.Acme.COM  ":               "t.acme.com",
		"t.acme.com.":                  "t.acme.com",
		"https://t.acme.com":           "t.acme.com",
		"https://t.acme.com/":          "t.acme.com",
		"http://t.acme.com/c/abc?x=1":  "t.acme.com",
		"https://user@t.acme.com/path": "t.acme.com",
		"localhost:3000":               "localhost:3000",
		"":                             "",
		"   ":                          "",
	}
	for in, want := range cases {
		if got := NormalizeTrackingHost(in); got != want {
			t.Errorf("NormalizeTrackingHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrackingURLScheme(t *testing.T) {
	cases := []struct{ host, want string }{
		// A production tracking host is a plain name: always https.
		{"t.acme.com", "https://t.acme.com/t/o/x.png"},
		{"t.acme.com:443", "https://t.acme.com:443/t/o/x.png"},
		// The local and LAN tracking services do not serve TLS, and an https
		// pixel there is one no browser can load.
		{"localhost:3000", "http://localhost:3000/t/o/x.png"},
		{"127.0.0.1:3000", "http://127.0.0.1:3000/t/o/x.png"},
		{"192.168.1.10:3000", "http://192.168.1.10:3000/t/o/x.png"},
		// No host means no tracking at all, and callers must not build a URL.
		{"", ""},
	}
	for _, c := range cases {
		if got := TrackingURL(c.host, "/t/o/x.png"); got != c.want {
			t.Errorf("TrackingURL(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

func TestTrackingURLNormalizesAndAnchorsPath(t *testing.T) {
	if got := TrackingURL("https://t.acme.com/", "c/abc"); got != "https://t.acme.com/c/abc" {
		t.Errorf("got %q", got)
	}
}

func TestTrackingHostReadsEnv(t *testing.T) {
	t.Setenv("TRACKING_DOMAIN", " HTTPS://T.Acme.com/ ")
	if got := TrackingHost(); got != "t.acme.com" {
		t.Errorf("TrackingHost() = %q", got)
	}
	if got := TrackingHostname(); got != "t.acme.com" {
		t.Errorf("TrackingHostname() = %q", got)
	}

	t.Setenv("TRACKING_DOMAIN", "localhost:3000")
	if got := TrackingHostname(); got != "localhost" {
		t.Errorf("TrackingHostname() = %q, want the name without the port", got)
	}
}
