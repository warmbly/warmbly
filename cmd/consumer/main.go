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
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
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

	kmsClient, err := kms.New(ctx, awscfg, masterKey)
	if err != nil {
		log.Fatal(err)
	}

	dynamoDB, err := dynamo.NewClient(ctx, awscfg)
	if err != nil {
		log.Fatal(err)
	}

	userEncryptedKeysRepo := repository.NewUserEncryptedKeysRepository(kmsClient, dynamoDB)
	cipherService := cipher.NewService(kmsClient, redisCache, userEncryptedKeysRepo)

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
	campaignRepo := repository.NewCampaignRepostory(primaryDB)
	taskRepo := repository.NewTaskRepository(primaryDB.Pool)
	contactRepo := repository.NewContactRepostory(primaryDB)
	campaignProgressRepo := repository.NewCampaignProgressRepository(primaryDB.Pool)
	crmRepo := repository.NewCRMRepository(primaryDB.Pool)
	advancedRepo := repository.NewAdvancedOutreachRepository(primaryDB.Pool)

	advancedService := advanced.NewService(
		advancedRepo,
		campaignRepo,
		emailRepo,
		taskRepo,
		contactRepo,
		campaignProgressRepo,
		crmRepo,
		nil,
		warmupService,
	)

	// Events publisher
	eventsPublisher := events.NewPublisher(kafkaProducer, s3Client, avrov2Client, cipherService)

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
		Publisher:                   eventsPublisher,
		StreamingPublisher:          streamingPublisher,
		AdvancedService:             advancedService,
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

	log.Println("Consumer started, listening on", kafka.TopicWorkerEvents)
	jobsService.Start(ctx)
	log.Println("Consumer stopped")
}
