package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/observability"
	"github.com/warmbly/warmbly/internal/repository"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Worker ID from hostname (UUID set by Terraform)
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal("failed to get hostname:", err)
	}
	workerID, err := uuid.Parse(hostname)
	if err != nil {
		// If hostname isn't a UUID, generate one for local dev
		workerID = uuid.New()
		log.Printf("Hostname %q is not a UUID, using generated ID: %s", hostname, workerID)
	} else {
		log.Printf("Worker ID from hostname: %s", workerID)
	}

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
	emailMessageMapRepo := repository.NewEmailMessageMapRepository(dynamoDB)

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

	// Kafka consumer — subscribe to worker-specific topic
	workerTopic := kafka.GetWorkerTopic(workerID.String())
	consumerConfig := kafka.NewConsumer(kafkaBootstrapServers)
	if kafkaSaslConfig != nil {
		consumerConfig.WithSASL(kafkaSaslConfig)
	}
	consumerConfig.Set("group.id", "worker-"+workerID.String())
	consumerConfig.Set("auto.offset.reset", "earliest")
	kafkaConsumer, err := consumerConfig.Connect()
	if err != nil {
		log.Fatal(err)
	}
	kafkaConsumer.WithAvrov2(avrov2Client)
	defer kafkaConsumer.Close()

	if err := kafkaConsumer.SubscribeTopics([]string{workerTopic}); err != nil {
		log.Fatal(err)
	}

	// WorkerService
	workerService := &worker.WorkerService{
		ID:                        workerID.String(),
		CipherService:             cipherService,
		KafkaProducer:             kafkaProducer,
		KafkaConsumer:             kafkaConsumer,
		Cache:                     redisCache,
		Storage:                   s3Client,
		EmailMessageMapRepository: emailMessageMapRepo,
	}

	if err := workerService.Init(); err != nil {
		log.Fatal("failed to init worker service:", err)
	}

	workerService.InitEvents()

	// Start heartbeat
	go workerService.Heartbeat(ctx)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down worker", workerID)
		cancel()
	}()

	log.Printf("Worker %s started, listening on topic %s", workerID, workerTopic)
	kafkaConsumer.Consume(ctx, workerService.Receive)
	log.Println("Worker stopped")
}
