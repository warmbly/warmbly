package kafka

import (
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
)

type Producer struct {
	p      *ckf.Producer
	Avrov2 *Avrov2
}

type ProducerConfig struct {
	config map[string]ckf.ConfigValue
}

func NewProducer(servers string) *ProducerConfig {
	return &ProducerConfig{
		config: map[string]ckf.ConfigValue{
			"bootstrap.servers": servers,
		},
	}
}

func (conf *ProducerConfig) WithSASL(saslConfig *SASLConfig) {
	for key, value := range saslConfig.Generate() {
		conf.Set(key, value)
	}
}

func (conf *ProducerConfig) Set(key string, value ckf.ConfigValue) {
	conf.config[key] = value
}

func (conf *ProducerConfig) Connect() (*Producer, error) {
	cm := &ckf.ConfigMap{}
	for k, v := range conf.config {
		if err := cm.SetKey(k, v); err != nil {
			return nil, err
		}
	}
	p, err := ckf.NewProducer(cm)
	if err != nil {
		return nil, err
	}

	return &Producer{
		p: p,
	}, nil
}

func (pr *Producer) WithAvrov2(avrov2 *Avrov2) {
	pr.Avrov2 = avrov2
}

func (pr *Producer) Close() {
	undelivered := pr.p.Flush(30_000) // 30 seconds
	if undelivered > 0 {
		log.Warn().Int("count", undelivered).Msg("messages not delivered during shutdown")
	}
	pr.p.Close()
}

func (pr *Producer) Produce(topic string, key, value []byte) error {
	return pr.p.Produce(&ckf.Message{
		TopicPartition: ckf.TopicPartition{Topic: &topic, Partition: ckf.PartitionAny},
		Key:            key,
		Value:          value,
		Timestamp:      time.Now(),
	}, nil)
}
