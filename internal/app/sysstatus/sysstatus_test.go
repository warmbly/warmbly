package sysstatus

import "testing"

func TestDialAddr(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare", "localhost:4222", "localhost:4222"},
		{"scheme", "nats://localhost:4222", "localhost:4222"},
		{"credentials", "nats://user:pass@nats.internal:4222", "nats.internal:4222"},
		{"password with at", "nats://user:p@ss@nats.internal:4222", "nats.internal:4222"},
		{"tls scheme", "tls://nats.internal:4222", "nats.internal:4222"},
		{"path", "nats://nats.internal:4222/cluster", "nats.internal:4222"},
		{"padded", "  broker:9092 ", "broker:9092"},
	}
	for _, c := range cases {
		if got := DialAddr(c.in); got != c.want {
			t.Errorf("%s: DialAddr(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// nats.go defaults a portless URL to 4222, so a probe that refuses to dial one
// reports the bus down while the client is happily connected.
func TestWithPort(t *testing.T) {
	cases := []struct {
		name       string
		addr, port string
		want       string
	}{
		{"portless gets the default", "nats.internal", "4222", "nats.internal:4222"},
		{"explicit port wins", "nats.internal:5222", "4222", "nats.internal:5222"},
		{"no default supplied", "nats.internal", "", "nats.internal"},
		{"empty address", "", "4222", ""},
		{"bracketed ipv6 with a port", "[::1]:4222", "4222", "[::1]:4222"},
		{"bracketed ipv6 without one", "[::1]", "4222", "[::1]:4222"},
		{"bare ipv6 is left alone", "::1", "4222", "::1"},
	}
	for _, c := range cases {
		if got := withPort(c.addr, c.port); got != c.want {
			t.Errorf("%s: withPort(%q, %q) = %q, want %q", c.name, c.addr, c.port, got, c.want)
		}
	}
}

// The whole path, as TCPCheck runs it.
func TestDialAddrWithPortEndToEnd(t *testing.T) {
	got := withPort(DialAddr("nats://user:pass@nats.internal"), "4222")
	if got != "nats.internal:4222" {
		t.Errorf("got %q, want nats.internal:4222", got)
	}
}
