package models

import (
	"time"

	"github.com/google/uuid"
)

// AWSCredentials is a named, reusable AWS keypair that one or more worker
// profiles can point at. The secret access key is stored encrypted; this
// struct never carries plaintext beyond the orchestrator's in-memory render.
type AWSCredentials struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Region      string    `json:"region"`
	AccessKeyID string    `json:"access_key_id"`

	// Hidden from JSON: this is ciphertext meant for the cipher service.
	SecretAccessKeyEncrypted string `json:"-"`

	// HasSecret is the dashboard-friendly signal that a secret is set,
	// without leaking the value. UI shows "••••••" when true.
	HasSecret bool `json:"has_secret"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkerProfile bundles everything a worker container needs at runtime
// besides its own identity. Many workers can share one profile.
type WorkerProfile struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`

	AppEnv      string `json:"app_env"`
	WorkerImage string `json:"worker_image"`

	KafkaBootstrapServers string `json:"kafka_bootstrap_servers"`
	KafkaSASLUsername     string `json:"kafka_sasl_username"`
	KafkaSASLPasswordEnc  string `json:"-"`
	HasKafkaPassword      bool   `json:"has_kafka_password"`

	SchemaRegistryURL    string `json:"schema_registry_url"`
	SchemaRegistryKey    string `json:"schema_registry_key"`
	SchemaRegistrySecEnc string `json:"-"`
	HasSchemaSecret      bool   `json:"has_schema_secret"`

	RedisURLEnc string `json:"-"`
	HasRedisURL bool   `json:"has_redis_url"`

	AWSCredentialID *uuid.UUID `json:"aws_credential_id,omitempty"`

	// Release tracking
	ReleaseChannel     ReleaseChannel `json:"release_channel"`
	AutoUpdate         bool           `json:"auto_update"`
	ResolvedImageTag   string         `json:"resolved_image_tag"`
	LastReleaseCheckAt *time.Time     `json:"last_release_check_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ReleaseChannel string

const (
	ReleaseChannelPinned ReleaseChannel = "pinned"
	ReleaseChannelStable ReleaseChannel = "stable"
	ReleaseChannelDev    ReleaseChannel = "dev"
)
