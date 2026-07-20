//go:build !kafka

// Default build: the Kafka backend is not compiled in, so the platform links no
// confluent-kafka-go / librdkafka and needs no CGO. Selecting
// EVENTBUS_PROVIDER=kafka here returns a clear error pointing at the `kafka`
// build tag.
package eventbus

import "errors"

// ErrKafkaNotCompiled is returned when the Kafka backend is selected in a build
// that did not include the `kafka` tag.
var ErrKafkaNotCompiled = errors.New("eventbus: kafka backend not compiled in; rebuild with -tags kafka (or use EVENTBUS_PROVIDER=nats)")

// NewKafka is the stub used in the default (CGO-free) build. The real
// implementation lives in kafka.go behind the `kafka` build tag.
func NewKafka(_ KafkaConfig) (EventBus, error) {
	return nil, ErrKafkaNotCompiled
}
