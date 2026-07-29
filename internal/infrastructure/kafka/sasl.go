package kafka

// SASLConfig carries Kafka SASL/PLAIN credentials. The struct is always
// compiled (referenced by eventbus.KafkaConfig and the service mains) even in
// the default CGO-free build; the confluent-specific Generate() method lives in
// sasl_kafka.go behind the `kafka` build tag.
type SASLConfig struct {
	Username string
	Password string
}
