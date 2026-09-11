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
