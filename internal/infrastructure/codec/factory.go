package codec

import (
	"fmt"
	"os"
)

// FromEnv constructs the active Codec from environment variables.
//
//	CODEC_PROVIDER=avro  -> NewAvro (requires SCHEMA_REGISTRY_URL,
//	                                 SCHEMA_REGISTRY_KEY,
//	                                 SCHEMA_REGISTRY_SECRET)
//	CODEC_PROVIDER=json  -> NewJSON
//	(unset)              -> defaults to "avro" for backwards compatibility
//
// Schema Registry inputs come from env rather than parameters so callers can
// stay codec-agnostic: a self-hoster who picks json will leave them unset and
// never reach the avro branch. Operators already running the historical Kafka
// + Schema Registry stack get the same behavior as before with no config
// change.
func FromEnv() (Codec, error) {
	provider := os.Getenv("CODEC_PROVIDER")
	if provider == "" {
		provider = "avro"
	}
	switch provider {
	case "avro":
		url := os.Getenv("SCHEMA_REGISTRY_URL")
		key := os.Getenv("SCHEMA_REGISTRY_KEY")
		secret := os.Getenv("SCHEMA_REGISTRY_SECRET")
		if url == "" {
			return nil, fmt.Errorf("codec: avro provider requires SCHEMA_REGISTRY_URL")
		}
		return NewAvro(url, key, secret)
	case "json":
		return NewJSON(), nil
	default:
		return nil, fmt.Errorf("codec: unknown CODEC_PROVIDER %q (want: avro, json)", provider)
	}
}
