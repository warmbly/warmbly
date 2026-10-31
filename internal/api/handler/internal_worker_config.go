package handler

import (
	"context"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

// Internal worker config endpoint. A worker starts with the env file it wrote
// at join time and pulls the rest from here on boot:
//
//	GET /api/v1/internal/worker/config -> WorkerConfig JSON
//
// Heartbeats go to the role-agnostic /internal/fleet/heartbeat instead, which
// is what tells a node the version it should be running.
//
// Auth: the node token (middleware.NodeBrokerAuthMiddleware).

type WorkerEgressConfig struct {
	ID       uuid.UUID `json:"id"`
	BindIP   string    `json:"bind_ip"`
	Hostname string    `json:"hostname"`
	Tags     []string  `json:"tags,omitempty"`
}

type WorkerKafkaConfig struct {
	Bootstrap   string `json:"bootstrap"`
	SASLUser    string `json:"sasl_user,omitempty"`
	SASLPass    string `json:"sasl_pass,omitempty"`
	SchemaURL   string `json:"schema_url,omitempty"`
	SchemaKey   string `json:"schema_key,omitempty"`
	SchemaSec   string `json:"schema_secret,omitempty"`
	WorkerTopic string `json:"worker_topic"`
}

type WorkerStorageConfig struct {
	EncryptedKeysProvider string `json:"encrypted_keys_provider"`
	EncryptedKeysBackend  string `json:"encrypted_keys_backend_url,omitempty"`
}

type WorkerConfig struct {
	WorkerID  uuid.UUID            `json:"worker_id"`
	BindIP    string               `json:"bind_ip,omitempty"`
	Tag       string               `json:"tag,omitempty"`
	Egresses  []WorkerEgressConfig `json:"egresses"`
	Kafka     WorkerKafkaConfig    `json:"kafka"`
	Storage   WorkerStorageConfig  `json:"storage"`
	EventBus  string               `json:"event_bus_provider"`
	BlobStore string               `json:"blob_store_provider"`
}

func (h *Handler) InternalWorkerConfig(c *gin.Context) {
	idParam := c.Query("worker_id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid worker_id query param required"})
		return
	}
	if !h.nodeInScope(c, "worker.config", http.StatusNotFound, func(ctx context.Context, node uuid.UUID) (bool, error) {
		return h.NodeScope.Worker(ctx, node, id)
	}) {
		return
	}
	bindIP := c.Query("bind_ip")
	tag := c.Query("tag")

	cfg := WorkerConfig{
		WorkerID: id,
		BindIP:   bindIP,
		Tag:      tag,
		Egresses: []WorkerEgressConfig{
			{
				ID:       id,
				BindIP:   bindIP,
				Hostname: tag,
			},
		},
		Kafka: WorkerKafkaConfig{
			Bootstrap:   envOr("KAFKA_BOOTSTRAP_SERVERS", ""),
			SASLUser:    envOr("KAFKA_SASL_USERNAME", ""),
			SASLPass:    envOr("KAFKA_SASL_PASSWORD", ""),
			SchemaURL:   envOr("SCHEMA_REGISTRY_URL", ""),
			SchemaKey:   envOr("SCHEMA_REGISTRY_KEY", ""),
			SchemaSec:   envOr("SCHEMA_REGISTRY_SECRET", ""),
			WorkerTopic: kafka.GetWorkerTopic(id.String()),
		},
		Storage: WorkerStorageConfig{
			EncryptedKeysProvider: envOr("ENCRYPTED_KEYS_PROVIDER", "http"),
			EncryptedKeysBackend:  envOr("ENCRYPTED_KEYS_BACKEND_URL", ""),
		},
		EventBus:  envOr("EVENTBUS_PROVIDER", "kafka"),
		BlobStore: envOr("BLOB_PROVIDER", "s3"),
	}
	c.JSON(http.StatusOK, cfg)
}

// envOr reads an environment variable with a fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
