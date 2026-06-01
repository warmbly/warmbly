package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/cipher"
	jobs "github.com/warmbly/warmbly/internal/app/consumer"
	"github.com/warmbly/warmbly/internal/app/integration"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/app/webhook"
	workerapp "github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/observability"
	"github.com/warmbly/warmbly/internal/repository"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load config with env-first approach
	cfg, err := config.NewConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Sentry
	if err := observability.InitSentry(ctx, cfg, "consumer"); err != nil {
		log.Fatal(err)
	}

	// AWS config for services that need it (KMS, S3, DynamoDB)
	awscfg, err := awsconf.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// PostgreSQL
	primaryDBEndpoint, err := cfg.LoadPrimaryDBEndpoint(ctx)
	if err != nil {
		log.Fatal(err)
	}
	primaryDB, err := db.New(ctx, primaryDBEndpoint)
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
		encryptedkeys.Deps{DB: primaryDB, Dynamo: dynamoDB},
		"postgres",
	)
	if err != nil {
		log.Fatal(err)
	}
	cipherService := cipher.NewService(kmsClient, redisCache, encryptedKeys)

	// S3
	s3Client, err := storage.NewClient(ctx, awscfg, "main")
	if err != nil {
		log.Fatal(err)
	}

	// Schema Registry → Avro v2
	schemaEndpoint, schemaKey, schemaSecret, err := cfg.LoadSchemaRegistryConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	avrov2Client, err := kafka.NewAvrov2Client(schemaEndpoint, schemaKey, schemaSecret)
	if err != nil {
		log.Fatal(err)
	}

	// Kafka bootstrap
	kafkaBootstrapServers, err := cfg.LoadKafkaBootstrapServers(ctx)
	if err != nil {
		log.Fatal(err)
	}
	kafkaSaslConfig, err := cfg.LoadKafkaConfigSasl(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Kafka producer
	producerConfig := kafka.NewProducer(kafkaBootstrapServers)
	if kafkaSaslConfig != nil {
		producerConfig.WithSASL(kafkaSaslConfig)
	}
	kafkaProducer, err := producerConfig.Connect()
	if err != nil {
		log.Fatal(err)
	}
	kafkaProducer.WithAvrov2(avrov2Client)
	defer kafkaProducer.Close()

	// Kafka consumer
	consumerConfig := kafka.NewConsumer(kafkaBootstrapServers)
	if kafkaSaslConfig != nil {
		consumerConfig.WithSASL(kafkaSaslConfig)
	}
	consumerConfig.Set("group.id", "consumer-group")
	consumerConfig.Set("auto.offset.reset", "earliest")
	kafkaConsumer, err := consumerConfig.Connect()
	if err != nil {
		log.Fatal(err)
	}
	kafkaConsumer.WithAvrov2(avrov2Client)
	defer kafkaConsumer.Close()

	if err := kafkaConsumer.SubscribeTopics([]string{kafka.TopicWorkerEvents}); err != nil {
		log.Fatal(err)
	}

	// Google Pub/Sub
	gcpProjectID := os.Getenv("GCP_PROJECT_ID")
	var streamingPublisher *pubsub.StreamingPublisher
	if gcpProjectID != "" {
		pubsubClient, err := pubsub.NewClient(ctx, gcpProjectID)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}
		defer pubsubClient.Close()
		streamingPublisher = pubsub.NewStreamingPublisher(pubsubClient)
	}

	// Repositories
	emailRepo := repository.NewEmailRepostory(primaryDB)
	uniboxRepo := repository.NewUniboxRepository(primaryDB)
	mailboxRepo := repository.NewMailboxRepository(primaryDB)
	emailHistoryIDRepo := repository.NewEmailHistoryIDRepository(dynamoDB)
	emailAccountErrorRepo := repository.NewEmailAccountErrorRepository(primaryDB)
	warmupRepo := repository.NewWarmupRepository(primaryDB.Pool)
	warmupService := warmupapp.NewService(warmupRepo)
	// Push warmup-health transitions live to the dashboard. The health sweep
	// runs in this process, so the realtime publisher is wired here.
	if streamingPublisher != nil {
		warmupService.WireRealtime(streamingPublisher, emailRepo)
	}
	workerRepo := repository.NewWorkerRepository(primaryDB.Pool)
	subscriptionRepoConsumer := repository.NewSubscriptionRepository(primaryDB.Pool)
	planRepoConsumer := repository.NewPlanRepository(primaryDB.Pool)
	workerAssignmentSvc := workerapp.NewAssignmentService(workerRepo, subscriptionRepoConsumer, planRepoConsumer)
	campaignRepo := repository.NewCampaignRepostory(primaryDB)
	taskRepo := repository.NewTaskRepository(primaryDB.Pool)
	contactRepo := repository.NewContactRepostory(primaryDB)
	campaignProgressRepo := repository.NewCampaignProgressRepository(primaryDB.Pool)
	crmRepo := repository.NewCRMRepository(primaryDB.Pool)
	advancedRepo := repository.NewAdvancedOutreachRepository(primaryDB.Pool)

	// Reply → integration fan-out. The consumer is where inbound replies are
	// detected, so this is where "prospect replied" turns into a Slack ping /
	// CRM upsert. webhookService.Dispatch enqueues customer webhook deliveries
	// (drained by the backend's DeliveryWorker) AND, via the wired sink, runs
	// integration actions in-process (cipher + Postgres are available here; the
	// consumer is control-plane, not a worker). Suppression already lives in the
	// advanced repo, so no separate suppression repo is wired here.
	webhookRepoC := repository.NewWebhookRepository(primaryDB.Pool)
	webhookService := webhook.NewService(webhookRepoC)
	integrationRepoC := repository.NewIntegrationRepository(primaryDB.Pool)
	integrationServiceC := integration.NewService(integrationRepoC, cipherService, integration.NewOAuthManager())
	webhookService.WireDispatchSink(integrationServiceC.DispatchAny)

	advancedService := advanced.NewService(
		advancedRepo,
		campaignRepo,
		emailRepo,
		taskRepo,
		contactRepo,
		campaignProgressRepo,
		crmRepo,
		nil, // tasksClient: the consumer does not schedule Cloud Tasks
		warmupService,
	)
	advancedService.WireDispatcher(webhookService)

	// Events publisher — wraps the existing Kafka producer in an EventBus,
	// wraps Avrov2 in a Codec. Once EVENTBUS_PROVIDER=nats is exercised in
	// prod, the kafkaProducer construction above can be deleted in favor of
	// constructing the bus via eventbus.FromEnv.
	consumerBus := eventbus.NewKafkaFromProducer(kafkaProducer, eventbus.KafkaConfig{
		Bootstrap: kafkaBootstrapServers,
		SASL:      kafkaSaslConfig,
	})
	consumerCodec := codec.NewAvroFromClient(avrov2Client)
	eventsPublisher := events.NewPublisher(consumerBus, s3Client, consumerCodec, cipherService)

	// JobsService
	jobsService := &jobs.JobsService{
		Consumer:                    kafkaConsumer,
		UniboxRepository:            uniboxRepo,
		MailboxRepository:           mailboxRepo,
		EmailRepository:             emailRepo,
		EmailHistoryIDRepository:    emailHistoryIDRepo,
		EmailAccountErrorRepository: emailAccountErrorRepo,
		WarmupRepo:                  warmupRepo,
		WarmupService:               warmupService,
		WorkerRepo:                  workerRepo,
		Publisher:                   eventsPublisher,
		StreamingPublisher:          streamingPublisher,
		AdvancedService:             advancedService,
		Cache:                       redisCache,
		AdminRepo:                   repository.NewAdminRepository(primaryDB.Pool),
		AssignmentService:           workerAssignmentSvc,
	}

	jobsService.InitEvents()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down consumer...")
		cancel()
	}()

	// Start DLQ auto-retry loop in background (every 60 seconds)
	go jobsService.StartDLQRetryLoop(ctx, 60*time.Second)

	// Start warmup health evaluation sweep (every hour)
	go jobsService.StartWarmupHealthSweep(ctx, 1*time.Hour)

	// Start dead worker detection (every 5 minutes)
	go jobsService.StartDeadWorkerDetection(ctx, 5*time.Minute)

	// Mirror Redis heartbeats into workers.last_seen_at every 60s
	// so the admin dashboard can render liveness without touching Redis.
	go jobsService.StartWorkerHeartbeatSync(ctx, 60*time.Second)

	// Re-evaluate per-mailbox risk bands hourly and migrate to a matching
	// risk_pool worker when the band changes. Skipped if AssignmentService
	// or WorkerRepo are nil.
	go jobsService.StartRiskRebalancer(ctx, 1*time.Hour)

	log.Println("Consumer started, listening on", kafka.TopicWorkerEvents)
	jobsService.Start(ctx)
	log.Println("Consumer stopped")
}
