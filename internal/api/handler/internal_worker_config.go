package handler

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Internal worker bootstrap + config endpoints. A worker process starts with
// a tiny 5-var envelope (WARMBLY_CONTROL_PLANE, WARMBLY_WORKER_TOKEN,
// WARMBLY_WORKER_TAG, optional WARMBLY_BIND_IPS, WARMBLY_LOG_LEVEL) and pulls
// the rest from here on boot:
//
//	GET  /api/v1/worker/config       -> WorkerConfig JSON
//	POST /api/v1/worker/heartbeat    -> 204, registers an egress heartbeat
//
// Auth: shared bearer token (INTERNAL_API_TOKEN), same as the DEK endpoint.
// Future (Task #9 follow-up): per-worker JWTs minted at registration time.

type WorkerEgressConfig struct {
	ID       uuid.UUID `json:"id"`
	BindIP   string    `json:"bind_ip"`
	Hostname string    `json:"hostname"`
	Tier     string    `json:"tier"`
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

// InternalWorkerConfig returns the runtime configuration for the calling
// worker. The worker passes its identity in query params:
//
//	?worker_id=<uuid>&bind_ip=<ip>&tag=<freeform>
//
// Today this returns a single-egress config built from the same env vars
// the backend itself uses. Once #6 (multi-egress per process) is fully
// wired the response will include all egresses assigned to this physical
// box, including any IP rotation events scheduled by the control plane.
func (h *Handler) InternalWorkerConfig(c *gin.Context) {
	idParam := c.Query("worker_id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid worker_id query param required"})
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
				Tier:     "shared",
			},
		},
		Kafka: WorkerKafkaConfig{
			Bootstrap:   envOr("KAFKA_BOOTSTRAP_SERVERS", ""),
			SASLUser:    envOr("KAFKA_SASL_USERNAME", ""),
			SASLPass:    envOr("KAFKA_SASL_PASSWORD", ""),
			SchemaURL:   envOr("SCHEMA_REGISTRY_URL", ""),
			SchemaKey:   envOr("SCHEMA_REGISTRY_KEY", ""),
			SchemaSec:   envOr("SCHEMA_REGISTRY_SECRET", ""),
			WorkerTopic: "w:" + id.String(),
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

// InternalWorkerHeartbeat records that a worker / egress is alive. Body is
// optional; query params identify the egress.
func (h *Handler) InternalWorkerHeartbeat(c *gin.Context) {
	// Heartbeat persistence happens through the existing worker repository.
	// Stubbed here — wire to repository.WorkerRepository.UpdateHeartbeat once
	// the egress data model lands fully (Task #6 follow-up).
	c.Status(http.StatusNoContent)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
