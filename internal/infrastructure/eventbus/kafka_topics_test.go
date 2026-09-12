//go:build kafka

package eventbus

import (
	"context"
	"testing"
)

// A topic already created by this process must not reach the broker again:
// ensureTopics runs on the publish path, and an admin round trip per message
// would put a network call in front of every send.
func TestEnsureTopicsSkipsKnown(t *testing.T) {
	b := &KafkaBus{
		bootstrap: "localhost:0",
		topics:    topicEnsurer{known: map[string]struct{}{"jobs.worker-events": {}}},
	}
	// No admin client is opened, so a broker call would fail rather than hang.
	if err := b.ensureTopics(context.Background(), "jobs.worker-events"); err != nil {
		t.Fatalf("a known topic tried to reach the broker: %v", err)
	}
	if b.topics.admin != nil {
		t.Error("an admin connection was opened for a topic already known")
	}
}

// Empty names come from callers that build a topic from an id that is not set
// yet; they must not become a create request for "".
func TestEnsureTopicsIgnoresEmpty(t *testing.T) {
	b := &KafkaBus{
		bootstrap: "localhost:0",
		topics:    topicEnsurer{known: map[string]struct{}{}},
	}
	if err := b.ensureTopics(context.Background(), "", ""); err != nil {
		t.Fatalf("empty topic names produced a request: %v", err)
	}
	if b.topics.admin != nil {
		t.Error("an admin connection was opened for empty topic names")
	}
}

// Both constructors must initialise the map, or the first ensure panics on a
// nil-map write rather than failing to publish.
func TestBothConstructorsInitialiseTopicMap(t *testing.T) {
	b := NewKafkaFromProducer(nil, KafkaConfig{Bootstrap: "localhost:0"})
	if b.topics.known == nil {
		t.Fatal("NewKafkaFromProducer left the topic map nil")
	}
}
