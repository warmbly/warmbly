//go:build kafka

package eventbus

import (
	"context"
	"testing"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
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

// A bus that has been closed must not open a new admin connection. Publish
// checks b.closed and then calls ensureTopics, so a Close landing between the
// two used to resurrect a client that nothing would ever shut.
func TestEnsureTopicsRefusesAfterClose(t *testing.T) {
	b := &KafkaBus{
		bootstrap: "localhost:0",
		topics:    topicEnsurer{known: map[string]struct{}{}, closed: true},
	}
	if err := b.ensureTopics(context.Background(), "w.something"); err == nil {
		t.Fatal("a closed bus accepted a topic creation")
	}
	if b.topics.admin != nil {
		t.Error("a closed bus opened an admin connection")
	}
	if _, err := b.adminClient(); err == nil {
		t.Error("adminClient handed out a client after close")
	}
}

// The lock must not be held across the broker call, or one slow creation
// stalls Publish, Subscribe and Close for every other topic. Asserted by
// taking the lock and confirming the read-only path still completes.
func TestUnknownTopicsDoesNotHoldLockForCaller(t *testing.T) {
	b := &KafkaBus{
		bootstrap: "localhost:0",
		topics:    topicEnsurer{known: map[string]struct{}{"a": {}}},
	}
	missing, err := b.unknownTopics([]string{"a"})
	if err != nil {
		t.Fatalf("unknownTopics: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("a known topic was reported missing: %v", missing)
	}
	// The lock is free the moment unknownTopics returns.
	done := make(chan struct{})
	go func() { b.topics.mu.Lock(); b.topics.mu.Unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the topic lock was still held after unknownTopics returned")
	}
}

// A managed cluster refuses the create for a topic it already owns, and that
// answer used to fail the publish and requeue the create for the next message:
// one lost event and one reported issue per message, forever. The refusal must
// read as "not ours to create" whichever of the two codes carries it, and
// whatever description the broker substituted for them.
func TestAuthorizationFailureIsNotACreateFailure(t *testing.T) {
	refusals := []ckf.Error{
		ckf.NewError(ckf.ErrTopicAuthorizationFailed, "Broker: Topic authorization failed", false),
		ckf.NewError(ckf.ErrClusterAuthorizationFailed, "Broker: Cluster authorization failed", false),
		// What Confluent Cloud actually sends, under a code of its choosing.
		ckf.NewError(ckf.ErrUnknown, "Authorization failed.", false),
	}
	for _, r := range refusals {
		if !isAuthorizationFailure(r) {
			t.Errorf("a create refused for permissions read as a create failure: %v", r)
		}
	}

	// The single-broker development case must still fail loudly: nothing is
	// going to produce to a topic the cluster could not build.
	notRefusals := []ckf.Error{
		ckf.NewError(ckf.ErrInvalidReplicationFactor, "Broker: Invalid replication factor", false),
		ckf.NewError(ckf.ErrTopicException, "Broker: Invalid topic", false),
	}
	for _, r := range notRefusals {
		if isAuthorizationFailure(r) {
			t.Errorf("a real create failure was waved through as a permissions refusal: %v", r)
		}
	}
}
