package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Internal worker bootstrap + config endpoints. A worker process starts with
// a tiny envelope on disk and pulls everything else from here on boot:
//
//	GET  /api/v1/worker/config       -> WorkerConfig JSON
//	POST /api/v1/worker/heartbeat    -> 204, auto-registers a new worker on
//	                                    first contact
//
// Auth: shared bearer token (INTERNAL_API_TOKEN). Future upgrade is per-worker
// JWTs minted at registration time so tier comes from token claims rather
// than the heartbeat body.

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

// HeartbeatPayload is what a worker sends on every heartbeat. The first
// heartbeat from an unknown WorkerID triggers auto-registration in the
// workers table; subsequent heartbeats just refresh last_seen.
type HeartbeatPayload struct {
	WorkerID   string `json:"worker_id"`
	BindIP     string `json:"bind_ip"`
	Tier       string `json:"tier,omitempty"`        // shared_free | shared_premium | dedicated
	EgressKind string `json:"egress_kind,omitempty"` // cold_smtp | oauth_api | warmup_only
}

func (h *Handler) InternalWorkerHeartbeat(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	var p HeartbeatPayload
	if err := json.Unmarshal(body, &p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decode body"})
		return
	}
	id, err := uuid.Parse(p.WorkerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid worker_id required"})
		return
	}
	if p.BindIP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bind_ip required"})
		return
	}
	if h.WorkerRepo == nil {
		// Worker repository not wired (e.g. tests). Treat as 204 noop.
		c.Status(http.StatusNoContent)
		return
	}
	if err := h.WorkerRepo.UpsertOnHeartbeat(c.Request.Context(), id, p.BindIP, p.Tier, p.EgressKind); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
