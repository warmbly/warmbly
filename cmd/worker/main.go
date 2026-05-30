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

	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
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

	// AWS config for services that need it (KMS, S3, DynamoDB)
	awscfg, err := awsconf.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
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

	// KMS + DynamoDB → CipherService
	var masterKey string = "alias/master-key"
	if cfg.Env != "prod" {
		masterKey += "-dev"
	}

	kmsClient, err := kms.FromEnv(ctx, awscfg, masterKey)
	if err != nil {
		log.Fatal(err)
	}

	dynamoDB, err := dynamo.NewClient(ctx, awscfg)
	if err != nil {
		log.Fatal(err)
	}

	encryptedKeys, err := encryptedkeys.FromEnv(
		encryptedkeys.Deps{Dynamo: dynamoDB},
		"http",
	)
	if err != nil {
		log.Fatal(err)
	}
	cipherService := cipher.NewService(kmsClient, redisCache, encryptedKeys)
	emailMessageMapRepo := repository.NewEmailMessageMapRepository(dynamoDB)

	// S3
	s3Client, err := storage.NewClient(ctx, awscfg, "main")
	if err != nil {
		log.Fatal(err)
	}

	// Codec (Avro by default, JSON when CODEC_PROVIDER=json).
	// Avro inputs come from the existing AWS Secrets Manager config so deploys
	// using Schema Registry don't need to duplicate them as env vars.
	var codecImpl codec.Codec
	if os.Getenv("CODEC_PROVIDER") == "json" {
		codecImpl = codec.NewJSON()
	} else {
		schemaEndpoint, schemaKey, schemaSecret, cerr := cfg.LoadSchemaRegistryConfig(ctx)
		if cerr != nil {
			log.Fatal(cerr)
		}
		avro, cerr := codec.NewAvro(schemaEndpoint, schemaKey, schemaSecret)
		if cerr != nil {
			log.Fatal(cerr)
		}
		codecImpl = avro
	}
	log.Printf("Codec: %s", codecImpl.Name())

	// Event bus (Kafka by default, NATS when EVENTBUS_PROVIDER=nats).
	kafkaBootstrapServers, err := cfg.LoadKafkaBootstrapServers(ctx)
	if err != nil {
		log.Fatal(err)
	}
	kafkaSaslConfig, err := cfg.LoadKafkaConfigSasl(ctx)
	if err != nil {
		log.Fatal(err)
	}
	bus, err := eventbus.FromEnv(kafkaBootstrapServers, kafkaSaslConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()
	log.Printf("Event bus: %s", bus.Name())

	// When the bus is Kafka-backed and the codec is Avro-backed, also wire
	// the underlying Avrov2 client into the Kafka producer so the existing
	// Avro wire format on Kafka topics is preserved.
	if kbus, ok := bus.(*eventbus.KafkaBus); ok {
		if ac, ok := codecImpl.(*codec.AvroCodec); ok {
			kbus.Producer().WithAvrov2(ac.Underlying())
		}
	}

	workerTopic := kafka.GetWorkerTopic(workerID.String())

	// WorkerService
	workerService := &worker.WorkerService{
		ID:                        workerID.String(),
		CipherService:             cipherService,
		Bus:                       bus,
		Codec:                     codecImpl,
		Cache:                     redisCache,
		Storage:                   s3Client,
		EmailMessageMapRepository: emailMessageMapRepo,
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
