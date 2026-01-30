package kafka

import (
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde/avrov2"
)

type Avrov2 struct {
	Ser   *avrov2.Serializer
	Deser *avrov2.Deserializer
}

func NewAvrov2Client(
	schemaRegistryAPIEndpoint, schemaRegistryAPIKey, schemaRegistryAPISecret string,
) (*Avrov2, error) {
	client, err := schemaregistry.NewClient(schemaregistry.NewConfigWithBasicAuthentication(
		schemaRegistryAPIEndpoint,
		schemaRegistryAPIKey,
		schemaRegistryAPISecret,
	))
	if err != nil {
		return nil, err
	}

	ser, err := avrov2.NewSerializer(client, serde.ValueSerde, avrov2.NewSerializerConfig())
	if err != nil {
		return nil, err
	}

	deser, err := avrov2.NewDeserializer(client, serde.ValueSerde, avrov2.NewDeserializerConfig())
	if err != nil {
		return nil, err
	}

	return &Avrov2{
		Ser:   ser,
		Deser: deser,
	}, nil
}
