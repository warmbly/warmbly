//go:build kafka

package eventbus

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
)

// Kafka has no equivalent of a NATS subject: a topic must exist before anything
// can be produced to it. Most of Warmbly's topics are fixed, but a worker's
// command topic is named after the node id it was given at join time, so the
// set is not knowable in advance and cannot be created by hand.
//
// Relying on the broker's auto.create.topics.enable is not enough: it is off by
// default on Confluent Cloud and not configurable below Standard, and a worker
// whose topic was never created receives no commands and reports no error.
//
// So the bus creates what it uses, once per topic per process.

const (
	topicPartitions        = 3
	topicReplicationFactor = 3
	topicAdminTimeout      = 20 * time.Second
)

type topicEnsurer struct {
	// mu guards known, admin and closed. It is never held across the broker
	// call: CreateTopics can wait the full admin timeout, and holding it there
	// would stall Publish, Subscribe and Close for every other topic.
	mu     sync.Mutex
	known  map[string]struct{}
	admin  *ckf.AdminClient
	closed bool
}

// ensureTopics creates any topic in names that this process has not already
// created. Creating one that exists is not an error: the broker answers
// TOPIC_ALREADY_EXISTS and that is the common case after the first call.
func (b *KafkaBus) ensureTopics(ctx context.Context, names ...string) error {
	missing, err := b.unknownTopics(names)
	if err != nil || len(missing) == 0 {
		return err
	}

	admin, err := b.adminClient()
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, topicAdminTimeout)
	defer cancel()
	// Deliberately outside the lock. Two callers racing to create the same
	// topic is harmless: the loser is told it already exists, which is the
	// steady state after the first call anyway.
	results, err := admin.CreateTopics(cctx, missing)
	if err != nil {
		return fmt.Errorf("eventbus kafka: create topics: %w", err)
	}

	created := make([]string, 0, len(results))
	for _, r := range results {
		switch r.Error.Code() {
		case ckf.ErrNoError:
			log.Info().Str("topic", r.Topic).Msg("eventbus kafka: topic created")
		case ckf.ErrTopicAlreadyExists:
			// The steady state on every process after the first.
		default:
			// An authorization failure is not a missing topic. A managed
			// cluster hands out a key with Write and Read and creates topics
			// from its own console, so it answers this for a topic that is
			// already there. Failing on it dropped every event and filed one
			// issue per message forever, because the topic never became known.
			// Remember it instead and let the produce decide: a topic that
			// really is absent fails there, saying exactly that.
			if isAuthorizationFailure(r.Error) {
				log.Warn().
					Str("topic", r.Topic).
					Msg("eventbus kafka: not allowed to create topics on this cluster; assuming it owns them")
				break
			}
			// A replication factor the cluster cannot satisfy is the usual
			// cause on a single-broker development cluster, and the bare
			// error does not say so.
			hint := ""
			if strings.Contains(strings.ToLower(r.Error.String()), "replication") {
				hint = " (the cluster has fewer brokers than the replication factor; this is expected on a single-broker development cluster)"
			}
			return fmt.Errorf("eventbus kafka: create topic %q: %s%s", r.Topic, r.Error.String(), hint)
		}
		created = append(created, r.Topic)
	}

	b.topics.mu.Lock()
	for _, t := range created {
		b.topics.known[t] = struct{}{}
	}
	b.topics.mu.Unlock()
	return nil
}

// isAuthorizationFailure reports whether the broker refused the create because
// this key may not make topics, rather than because the create itself was bad.
//
// The two codes are the answer; the description is a fallback because Confluent
// Cloud substitutes its own "Authorization failed." for them and the code that
// arrives with it is not documented. The fallback is the whole description and
// not a substring on purpose: this waves a topic through as created, so an error
// that merely mentions authorization must not be mistaken for a refusal to
// create one, or the topic is remembered as present and every later publish to
// it fails for a reason nothing reported.
func isAuthorizationFailure(err ckf.Error) bool {
	switch err.Code() {
	case ckf.ErrTopicAuthorizationFailed, ckf.ErrClusterAuthorizationFailed:
		return true
	}
	desc := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(err.String())), ".")
	return desc == "authorization failed"
}

// unknownTopics returns specs for the names this process has not created yet.
// Short critical section: no network happens under the lock.
func (b *KafkaBus) unknownTopics(names []string) ([]ckf.TopicSpecification, error) {
	b.topics.mu.Lock()
	defer b.topics.mu.Unlock()
	if b.topics.closed {
		return nil, ErrBusClosed
	}
	var missing []ckf.TopicSpecification
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, seen := b.topics.known[n]; seen {
			continue
		}
		missing = append(missing, ckf.TopicSpecification{
			Topic:             n,
			NumPartitions:     topicPartitions,
			ReplicationFactor: topicReplicationFactor,
		})
	}
	return missing, nil
}

// adminClient opens the admin connection lazily, so a deployment that never
// needs to create a topic never opens one.
func (b *KafkaBus) adminClient() (*ckf.AdminClient, error) {
	b.topics.mu.Lock()
	defer b.topics.mu.Unlock()
	// Close marks this before it clears the client, so a Publish that raced
	// past the closed check cannot open a replacement nothing will shut.
	if b.topics.closed {
		return nil, ErrBusClosed
	}
	if b.topics.admin != nil {
		return b.topics.admin, nil
	}
	conf := &ckf.ConfigMap{"bootstrap.servers": b.bootstrap}
	if b.sasl != nil {
		// The same renderer the producer and consumer use, so the admin
		// connection cannot authenticate differently from the traffic.
		for k, v := range b.sasl.Generate() {
			_ = conf.SetKey(k, v)
		}
	}
	admin, err := ckf.NewAdminClient(conf)
	if err != nil {
		return nil, fmt.Errorf("eventbus kafka: admin client: %w", err)
	}
	b.topics.admin = admin
	return admin, nil
}
