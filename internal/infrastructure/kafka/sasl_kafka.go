//go:build kafka

package kafka

import (
	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Generate renders the SASL/PLAIN settings as confluent config values. Only
// compiled with the `kafka` build tag (the producer/consumer that consume it
// are Kafka-only).
func (c *SASLConfig) Generate() map[string]ckf.ConfigValue {
	return map[string]ckf.ConfigValue{
		"sasl.mechanisms":   "PLAIN",
		"security.protocol": "SASL_SSL",
		"sasl.username":     c.Username,
		"sasl.password":     c.Password,
	}
}
