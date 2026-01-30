package kafka

import (
	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type SASLConfig struct {
	Username string
	Password string
}

func (c *SASLConfig) Generate() map[string]ckf.ConfigValue {
	return map[string]ckf.ConfigValue{
		"sasl.mechanisms":   "PLAIN",
		"security.protocol": "SASL_SSL",
		"sasl.username":     c.Username,
		"sasl.password":     c.Password,
	}
}
