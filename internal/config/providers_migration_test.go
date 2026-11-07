package config

import "testing"

func TestEventBusUsesKafkaDuringNATSMigration(t *testing.T) {
	for _, tc := range []struct {
		provider, both string
		want           bool
	}{
		{"", "", true}, {"kafka", "", true}, {"nats", "", false},
		{"nats", "true", true}, {"nats", "false", false},
	} {
		t.Setenv("EVENTBUS_PROVIDER", tc.provider)
		t.Setenv("EVENTBUS_READ_BOTH", tc.both)
		if got := EventBusUsesKafka(); got != tc.want {
			t.Fatalf("provider=%q both=%q: %v", tc.provider, tc.both, got)
		}
	}
}
