package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/observability"
	"github.com/warmbly/warmbly/internal/repository"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve worker identity. Precedence:
	//   1. WORKER_ID:        explicit UUID, used as-is
	//   2. WORKER_BIND_IP:   derive UUIDv5 from the bound egress IP so each
	//                        IP on a multi-IP box becomes its own worker
	//   3. hostname:         UUID set by Terraform on legacy single-IP VPS
	//   4. generated UUID:   local dev fallback
	workerID, bindIP := resolveWorkerID()
	log.Printf("Worker ID: %s, Bind IP: %s", workerID, bindIP)

	// Load config with env-first approach
	cfg, err := config.NewConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Sentry
	if err := observability.InitSentry(ctx, cfg, "worker"); err != nil {
		log.Fatal(err)
	}

	// AWS SDK config, loaded only when an AWS-backed provider is selected
	// (KMS_PROVIDER=aws or BLOB_PROVIDER=s3). A fully-local self-host needs no
	// AWS_REGION or credentials.
	var awscfg aws.Config
	if config.AWSNeeded() {
		awscfg, err = awsconf.LoadDefaultConfig(ctx)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Redis
	primaryRedis, err := cfg.LoadPrimaryRedisEndpoint(ctx)
	if err != nil {
		log.Fatal(err)
	}
	redisCache, err := cache.New(primaryRedis)
	if err != nil {
		log.Fatal(err)
	}

	// KMS → CipherService
	var masterKey string = "alias/master-key"
	if cfg.Env != "prod" {
		masterKey += "-dev"
	}

	kmsClient, err := kms.FromEnv(ctx, awscfg, masterKey)
	if err != nil {
		log.Fatal(err)
	}

	encryptedKeys, err := encryptedkeys.FromEnv(
		encryptedkeys.Deps{},
		"http",
	)
	if err != nil {
		log.Fatal(err)
	}
	cipherService := cipher.NewService(kmsClient, redisCache, encryptedKeys)

	// The worker reaches the messageId -> internal email map over the internal
	// backend API (same base URL + token as the DEK store) rather than touching
	// Postgres directly, per the worker no-direct-SQL rule in CLAUDE.md.
	internalBaseURL := strings.TrimRight(os.Getenv("ENCRYPTED_KEYS_BACKEND_URL"), "/")
	internalToken := os.Getenv("ENCRYPTED_KEYS_WORKER_TOKEN")
	emailMessageMapRepo, err := repository.NewHTTPEmailMessageMapRepository(internalBaseURL, internalToken)
	if err != nil {
		log.Fatal(err)
	}

	// Blob storage (S3 by default, filesystem when BLOB_PROVIDER=filesystem).
	s3Client, err := storage.NewFromEnv(ctx, awscfg, "main")
	if err != nil {
		log.Fatal(err)
	}

	// Codec (Avro reads SCHEMA_REGISTRY_URL from env; JSON needs nothing).
	codecImpl, err := codec.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Codec: %s", codecImpl.Name())

	// Event bus (NATS default; Kafka when EVENTBUS_PROVIDER=kafka). Kafka
	// bootstrap/SASL is only loaded for the Kafka provider.
	var kafkaBootstrapServers string
	var kafkaSaslConfig *kafka.SASLConfig
	if config.EventBusProvider() == "kafka" {
		kafkaBootstrapServers, err = cfg.LoadKafkaBootstrapServers(ctx)
		if err != nil {
			log.Fatal(err)
		}
		kafkaSaslConfig, err = cfg.LoadKafkaConfigSasl(ctx)
		if err != nil {
			log.Fatal(err)
		}
	}
	bus, err := eventbus.FromEnv(kafkaBootstrapServers, kafkaSaslConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()
	log.Printf("Event bus: %s", bus.Name())

	workerTopic := kafka.GetWorkerTopic(workerID.String())

	// Provider OAuth configs for local token refresh. Cfg is not shipped in the
	// AddWorkerEmail payload (avro-excluded), so the worker rebuilds it from
	// these (reads BOX_GOOGLE_* / BOX_OUTLOOK_* from the worker env). RedirectURL
	// is unused for refresh, so the base URL is irrelevant here.
	oauthInbox := config.LoadOauth2Inbox("")
	// Token refresh needs the provider client credentials in the worker env.
	// Warn loudly if they're missing: the initial token still works, but refresh
	// fails silently once it expires (~1h), stalling the mailbox.
	if oauthInbox.Outlook == nil || oauthInbox.Outlook.ClientID == "" || oauthInbox.Outlook.ClientSecret == "" {
		log.Println("WARNING: BOX_OUTLOOK_CLIENT_ID/SECRET not set; Microsoft Graph mailbox token refresh will fail on expiry")
	}
	if oauthInbox.Google == nil || oauthInbox.Google.ClientID == "" || oauthInbox.Google.ClientSecret == "" {
		log.Println("WARNING: BOX_GOOGLE_CLIENT_ID/SECRET not set; Gmail mailbox token refresh will fail on expiry")
	}

	// WorkerService
	workerService := &worker.WorkerService{
		ID:                        workerID.String(),
		CipherService:             cipherService,
		Bus:                       bus,
		Codec:                     codecImpl,
		Cache:                     redisCache,
		Storage:                   s3Client,
		EmailMessageMapRepository: emailMessageMapRepo,
		OauthInbox:                &oauthInbox,
	}

	if err := workerService.Init(); err != nil {
		log.Fatal("failed to init worker service:", err)
	}

	workerService.InitEvents()

	// Start heartbeat + health sampler. RunHealth ticks every 30s, snapshots
	// the rolling 1m counters into a WorkerHealth event, publishes via the
	// event bus so the consumer can write a row into worker_health_samples.
	go workerService.Heartbeat(ctx)
	go runInternalHeartbeat(ctx, workerID, bindIP)
	go workerService.RunHealth(ctx, 30*time.Second)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down worker", workerID)
		cancel()
	}()

	log.Printf("Worker %s started, listening on topic %s", workerID, workerTopic)
	if err := bus.Subscribe(ctx, []string{workerTopic}, "worker-"+workerID.String(), workerService.Receive); err != nil {
		log.Println("event bus subscribe ended:", err)
	}
	log.Println("Worker stopped")
}

func runInternalHeartbeat(ctx context.Context, workerID uuid.UUID, bindIP string) {
	baseURL := strings.TrimRight(os.Getenv("ENCRYPTED_KEYS_BACKEND_URL"), "/")
	token := os.Getenv("ENCRYPTED_KEYS_WORKER_TOKEN")
	if baseURL == "" || token == "" {
		return
	}
	reportedIP := os.Getenv("WORKER_PUBLIC_IP")
	if reportedIP == "" && bindIP != "default route" {
		reportedIP = bindIP
	}
	if reportedIP == "" {
		reportedIP = "unknown"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	send := func() {
		payload := map[string]string{
			"worker_id":   workerID.String(),
			"bind_ip":     reportedIP,
			"tier":        os.Getenv("WORKER_TIER"),
			"egress_kind": os.Getenv("WORKER_EGRESS_KIND"),
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/internal/worker/heartbeat", bytes.NewReader(body))
		if err != nil {
			log.Println("failed to build internal heartbeat:", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			log.Println("failed internal heartbeat:", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Println("internal heartbeat returned status", resp.StatusCode)
		}
	}

	send()
	ticker := time.NewTicker(90 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

// uuidNamespaceURL is the RFC 4122 URL namespace, matching the value used by
// scripts/install-worker.sh when deriving the per-IP worker ID. Keep these in
// sync: the installer and the worker must agree on the derivation.
var uuidNamespaceURL = uuid.MustParse("6ba7b811-9dad-11d1-80b4-00c04fd430c8")

// workerIDFromIP returns the deterministic UUIDv5 for the given IPv4 string.
// Same IP always maps to the same UUID, which is what lets us treat each IP
// on a multi-IP box as its own stable sending identity (egress).
func workerIDFromIP(ip string) uuid.UUID {
	return uuid.NewSHA1(uuidNamespaceURL, []byte(ip))
}

// resolveWorkerID applies the boot-time precedence rules and returns the
// chosen worker UUID together with a human-readable bind-IP label for logs.
func resolveWorkerID() (uuid.UUID, string) {
	if raw := os.Getenv("WORKER_ID"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			log.Fatalf("WORKER_ID %q is not a valid UUID: %v", raw, err)
		}
		bind := os.Getenv("WORKER_BIND_IP")
		if bind == "" {
			bind = "default route"
		}
		return id, bind
	}

	if bind := os.Getenv("WORKER_BIND_IP"); bind != "" {
		return workerIDFromIP(bind), bind
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal("failed to get hostname:", err)
	}
	if id, err := uuid.Parse(hostname); err == nil {
		return id, "default route"
	}
	id := uuid.New()
	log.Printf("Hostname %q is not a UUID, using generated ID: %s", hostname, id)
	return id, "default route"
}
