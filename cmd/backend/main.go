package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/getsentry/sentry-go"
	"github.com/meszmate/apple-go"
	"github.com/meszmate/google-go"
	"github.com/warmbly/warmbly/internal/api"
	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/admin"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/crm"
	"github.com/warmbly/warmbly/internal/app/dangerzone"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/fleet"
	"github.com/warmbly/warmbly/internal/app/group"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/releases"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/settings"
	"github.com/warmbly/warmbly/internal/app/socket"
	"github.com/warmbly/warmbly/internal/app/stripe"
	"github.com/warmbly/warmbly/internal/app/subscription"
	"github.com/warmbly/warmbly/internal/app/template"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/app/user"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/app/worker_orchestrator"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/jobs"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/observability"
	"github.com/warmbly/warmbly/internal/pkg/captcha"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks"
)

func main() {
	var addr string
	var ginMode string
	var websocketURI string
	var allowedOrigins []string

	var tzService tz.TzService

	var serviceAccount string
	var keySet keyfunc.Keyfunc

	var tokenService token.TokenService
	var authService auth.AuthService
	var userService user.UserService
	var emailService email.EmailService
	var campaignService campaign.CampaignService
	var sequenceService sequence.SequenceService
	var contactService contact.ContactService
	var socketService socket.SocketService
	var uniboxService unibox.UniboxService
	var cipherService cipher.CipherService
	var encryptedKeys encryptedkeys.Store
	var storageBackendRepo repository.StorageBackendRepository
	var cloudCredentialRepo repository.CloudCredentialRepository
	var provisioningTemplateRepo repository.ProvisioningTemplateRepository
	var provisioningJobRepo repository.ProvisioningJobRepository
	var provisioningPolicyRepo repository.ProvisioningPolicyRepository
	var tasksService tasks.TasksService
	var advancedService advanced.Service

	var folderService group.GroupService
	var tagService group.GroupService
	var categoryService group.GroupService
	var crmService crm.CRMService
	var apiKeyService apikey.APIKeyService

	// New services for trial, feature gates, and worker assignment
	var trialService trial.TrialService
	var featureGateService feature.FeatureGateService
	var workerAssignmentService worker.WorkerAssignmentService
	var subscriptionService subscription.SubscriptionService
	var stripeService stripe.StripeService
	var organizationService organization.OrganizationService

	// Email send & templates
	var templateService template.TemplateService
	var emailSendService emailsend.EmailSendService

	// Admin
	var adminService admin.AdminService

	// Worker orchestrator (SSH-driven admin worker lifecycle)
	var workerOrchestrator *worker_orchestrator.Orchestrator
	var workerRepoForHandler repository.WorkerRepository
	var credentialsRepository repository.CredentialsRepository
	var releasesService *releases.Service

	// Notifications
	var emailNotificationService notify.EmailNotificationService

	// Warmup
	var warmupService warmupapp.Service

	// Danger zone (delayed deletions)
	var dangerZoneService dangerzone.Service

	// Pub/Sub for realtime streaming
	var streamingPublisher *pubsub.StreamingPublisher

	// Surfaced into the handler for avatar uploads and other direct
	// repository / object-storage needs. Declared up here so they
	// survive the config block where they're initialized.
	var s3ForHandler *storage.Client
	var userRepoForHandler repository.UserRepository
	var organizationRepoForHandler repository.OrganizationRepository
	var warmupRoutingRepoForHandler repository.WarmupRoutingRepository
	var webhookServiceForHandler webhook.Service

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	{

		// Load config with env-first approach
		cfg, err := config.NewConfig(ctx)
		if err != nil {
			log.Fatal(err)
		}

		if err := observability.InitSentry(ctx, cfg, "backend"); err != nil {
			log.Fatal(err)
		}

		serviceAccount, err = cfg.LoadGoogleServiceAccount(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		keySet, err = keyfunc.NewDefaultCtx(ctx, []string{"https://www.googleapis.com/oauth2/v3/certs"})
		if err != nil {
			if cfg.Env == "dev" {
				log.Printf("Warning: Failed to fetch Google OIDC keys: %v", err)
			} else {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
		}

		apiCfg, err := cfg.LoadApiConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// AWS config for services that need it (KMS, S3, DynamoDB)
		awscfg, err := awsconf.LoadDefaultConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		var masterKey string = "alias/master-key"
		if cfg.Env != "prod" {
			masterKey += "-dev"
		}

		kms, err := kms.FromEnv(ctx, awscfg, masterKey)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		geoPath, err := cfg.LoadGeoDBPath(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		var geoloc *geo.Client
		geoloc, err = geo.New(geoPath)
		if err != nil {
			if cfg.Env == "dev" {
				log.Printf("Warning: GeoIP database not found at %s, geo lookups disabled", geoPath)
			} else {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
		}

		s3, err := storage.NewClient(ctx, awscfg, "main")
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}
		s3ForHandler = s3

		primaryDBEndpoint, err := cfg.LoadPrimaryDBEndpoint(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		primaryDB, err := db.New(ctx, primaryDBEndpoint)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// Run database migrations
		log.Println("Running database migrations...")
		if err := db.RunMigrations(primaryDBEndpoint); err != nil {
			sentry.CaptureException(err)
			log.Fatal("Failed to run migrations: ", err)
		}
		log.Println("Database migrations completed")

		dynamoDB, err := dynamo.NewClient(ctx, awscfg)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		primaryRedis, err := cfg.LoadPrimaryRedisEndpoint(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		cache, err := cache.New(primaryRedis)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// Google Pub/Sub for realtime streaming (optional)
		gcpProjectID := os.Getenv("GCP_PROJECT_ID")
		if gcpProjectID != "" {
			pubsubClient, err := pubsub.NewClient(ctx, gcpProjectID)
			if err != nil {
				sentry.CaptureException(err)
				log.Printf("Warning: Failed to initialize Pub/Sub client: %v", err)
			} else {
				streamingPublisher = pubsub.NewStreamingPublisher(pubsubClient)
			}
		}

		emailCfg, err := cfg.LoadEmailConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		smtpCfg := cfg.LoadSMTPConfig(ctx)
		if smtpCfg != nil {
			emailNotificationService = notify.NewSMTPEmailNotificationService(
				emailCfg.EmailName,
				emailCfg.EmailAddress,
				smtpCfg.Host,
				smtpCfg.Port,
			)
		} else {
			emailNotificationService, err = notify.NewEmailNotficiationService(
				ctx,
				emailCfg.EmailName,
				emailCfg.EmailAddress,
			)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
		}

		authCfg, err := cfg.LoadAuthConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		googleAuth := google.NewAuth(
			authCfg.GoogleClientID,
			authCfg.GoogleClientSecret,
			authCfg.GoogleRedirectURI,
			nil,
		)

		var appleAuthClient apple.AppleAuth
		appleAuthInstance, appleErr := apple.NewB64(
			authCfg.AppleAppID,
			authCfg.AppleTeamID,
			authCfg.AppleKeyID,
			authCfg.AppleKeySecret,
		)
		if appleErr != nil {
			if cfg.Env == "dev" {
				log.Printf("Warning: Apple auth initialization failed (expected in dev): %v", appleErr)
			} else {
				sentry.CaptureException(appleErr)
				log.Fatal(appleErr)
			}
		} else {
			appleAuthClient = appleAuthInstance
		}

		kafkaBootstrapServers, err := cfg.LoadKafkaBootstrapServers(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		kafkaSaslConfig, err := cfg.LoadKafkaConfigSasl(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		schemaEndpoint, schemaKey, schemaSecret, err := cfg.LoadSchemaRegistryConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		avrov2Client, err := kafka.NewAvrov2Client(schemaEndpoint, schemaKey, schemaSecret)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// Codec wraps the same Avrov2 client so EventBus payloads decode the
		// same way regardless of transport.
		codecImpl := codec.NewAvroFromClient(avrov2Client)

		// Legacy Kafka producer still used by email + tasks services that
		// haven't been migrated to EventBus yet. Removing this is follow-up
		// work after the EventBus wiring stabilizes.
		kafkaProducerConfig := kafka.NewProducer(kafkaBootstrapServers)
		if kafkaSaslConfig != nil {
			kafkaProducerConfig.WithSASL(kafkaSaslConfig)
		}
		kafkaProducer, err := kafkaProducerConfig.Connect()
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}
		kafkaProducer.WithAvrov2(avrov2Client)

		// Event bus. Today this is Kafka in production; flip to NATS by
		// setting EVENTBUS_PROVIDER=nats and NATS_URL.
		bus, err := eventbus.FromEnv(kafkaBootstrapServers, kafkaSaslConfig)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// Preserve Kafka wire format when both are Kafka-backed + Avro-coded.
		if kbus, ok := bus.(*eventbus.KafkaBus); ok {
			kbus.Producer().WithAvrov2(avrov2Client)
		}

		turnstileBypassToken := ""
		if cfg.Env == "dev" {
			turnstileBypassToken = authCfg.TurnstileBypass
			if turnstileBypassToken == "" {
				turnstileBypassToken = "warmbly-local-turnstile-bypass"
			}
		}

		captcha := captcha.NewTurnstileFromConfig(captcha.TurnstileConfig{
			Secret:      authCfg.TurnstileSecret,
			BypassToken: turnstileBypassToken,
		})

		userRepostory := repository.NewUserRepostory(primaryDB, kms)
		userRepoForHandler = userRepostory
		authRepostory := repository.NewAuthRepostory(primaryDB)
		tokenRepostory := repository.NewTokenRepostory(primaryDB)
		emailRepostory := repository.NewEmailRepostory(primaryDB)
		campaignRepostory := repository.NewCampaignRepostory(primaryDB)
		sequenceRepostory := repository.NewSequenceRepostory(primaryDB)
		contactRepostory := repository.NewContactRepostory(primaryDB)
		uniboxRepository := repository.NewUniboxRepository(primaryDB)
		encryptedKeys, err = encryptedkeys.FromEnv(
			encryptedkeys.Deps{DB: primaryDB, Dynamo: dynamoDB},
			"postgres",
		)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		folderRepostory := repository.NewGroupRepostory(primaryDB, models.Folders)
		tagRepostory := repository.NewGroupRepostory(primaryDB, models.Tags)
		categoryRepostory := repository.NewGroupRepostory(primaryDB, models.Categories)

		// New repositories for subscription & worker management
		subscriptionRepository := repository.NewSubscriptionRepository(primaryDB.Pool)
		planRepository := repository.NewPlanRepository(primaryDB.Pool)
		workerRepository := repository.NewWorkerRepository(primaryDB.Pool)
		organizationRepository := repository.NewOrganizationRepository(primaryDB.Pool)
		organizationRepoForHandler = organizationRepository
		taskRepository := repository.NewTaskRepository(primaryDB.Pool)
		apiKeyRepository := repository.NewAPIKeyRepository(primaryDB)
		crmRepository := repository.NewCRMRepository(primaryDB.Pool)
		advancedRepository := repository.NewAdvancedOutreachRepository(primaryDB.Pool)
		templateRepository := repository.NewTemplateRepository(primaryDB.Pool)
		warmupRepository := repository.NewWarmupRepository(primaryDB.Pool)
		warmupRoutingRepository := repository.NewWarmupRoutingRepository(primaryDB.Pool)
		warmupRoutingRepoForHandler = warmupRoutingRepository
		webhookRepository := repository.NewWebhookRepository(primaryDB.Pool)
		webhookService := webhook.NewService(webhookRepository)
		webhookServiceForHandler = webhookService

		// Drain the webhook delivery queue in-process. Multiple replicas are
		// safe because ClaimDueDeliveries uses SELECT … FOR UPDATE SKIP LOCKED.
		webhookWorker := webhook.NewDeliveryWorker(webhookRepository, webhook.DeliveryWorkerOptions{})
		go webhookWorker.Run(ctx)
		campaignProgressRepository := repository.NewCampaignProgressRepository(primaryDB.Pool)
		campaignLogRepository := repository.NewCampaignLogRepository(primaryDB)
		warmupService = warmupapp.NewService(warmupRepository)
		// Fan out warmup health transitions to customer webhooks.
		warmupService.WireWebhooks(webhookService, emailRepostory)

		tzService = tz.NewService()

		// Initialize new services for trial, feature gates, and worker assignment
		trialService = trial.NewService(subscriptionRepository, userRepostory)
		featureGateService = feature.NewService(subscriptionRepository, planRepository)
		workerAssignmentService = worker.NewAssignmentService(workerRepository, subscriptionRepository, planRepository)
		subscriptionService = subscription.NewService(subscriptionRepository, planRepository)
		organizationService = organization.NewService(organizationRepository, subscriptionRepository, userRepostory)

		// Load Stripe config and initialize service
		stripeCfg, err := cfg.LoadStripeConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}
		stripeService = stripe.NewService(stripeCfg, subscriptionRepository, planRepository, workerAssignmentService)

		tokenService = token.NewService(primaryDB, tokenRepostory, cache, geoloc, authCfg.AuthSecret)
		userService = user.NewService(userRepostory, cache)
		authService = auth.NewService(
			authRepostory,
			cache,
			captcha,
			tokenService,
			emailNotificationService,
			&models.ExternalAuth{
				GoogleAuth: googleAuth,
				AppleAuth:  appleAuthClient,
			},
			trialService,
			organizationService,
			userRepostory,
			userService,
		)
		cipherService = cipher.NewService(kms, cache, encryptedKeys)

		// Reflect the active infrastructure backends into storage_backends so
		// the admin UI can display what's running. Read-only entries — they
		// were chosen via env vars and changing them at runtime would orphan
		// existing ciphertext / DEKs.
		storageBackendRepo = repository.NewStorageBackendRepository(primaryDB)
		cloudCredentialRepo = repository.NewCloudCredentialRepository(primaryDB)
		provisioningTemplateRepo = repository.NewProvisioningTemplateRepository(primaryDB)
		provisioningJobRepo = repository.NewProvisioningJobRepository(primaryDB)
		provisioningPolicyRepo = repository.NewProvisioningPolicyRepository(primaryDB)
		settingsRegistrar := settings.NewRegistrar(storageBackendRepo)
		if err := settingsRegistrar.RegisterAll(ctx, []settings.Backend{
			{Kind: "kms", Provider: kms.Name(), Display: kms.Name(), ReadOnly: true},
			{Kind: "encrypted_keys", Provider: encryptedKeys.Name(), Display: encryptedKeys.Name(), ReadOnly: true},
			{Kind: "blob", Provider: s3.Name(), Display: s3.Name(), ReadOnly: true},
			{Kind: "eventbus", Provider: "kafka", Display: "kafka", ReadOnly: true},
		}); err != nil {
			sentry.CaptureException(err)
			log.Printf("storage_backends registrar: %v", err)
		}

		// Autonomous fleet management background loops. Each runs on its own
		// interval and writes every action to decision_log. Cancel them via
		// the root context on shutdown.
		decisionLogRepo := repository.NewDecisionLogRepository(primaryDB)

		// Refresh worker_capacity_view every minute so the assignment loop +
		// rebalance + scale + quarantine evaluators see fresh rolling
		// metrics. The materialized view is what aggregates the 1h windows
		// across all workers.
		go func() {
			tick := time.NewTicker(time.Minute)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					if err := workerRepository.RefreshWorkerCapacityView(ctx); err != nil {
						log.Printf("worker_capacity_view refresh: %v", err)
					}
				}
			}
		}()

		go (&fleet.Rebalancer{
			WorkerRepo: workerRepository,
			Decisions:  decisionLogRepo,
		}).Run(ctx)
		go (&fleet.Scaler{
			WorkerRepo:   workerRepository,
			PolicyRepo:   provisioningPolicyRepo,
			TemplateRepo: provisioningTemplateRepo,
			JobRepo:      provisioningJobRepo,
			Decisions:    decisionLogRepo,
		}).Run(ctx)
		go (&fleet.QuarantineEvaluator{
			WorkerRepo: workerRepository,
			Decisions:  decisionLogRepo,
		}).Run(ctx)

		// Worker orchestrator. The env config below is the FALLBACK that gets
		// written into /etc/warmbly/worker.env when a worker has no profile
		// assigned. Production workers should reference a worker_profile row;
		// dev/sim can rely on the fallback so docker-compose still works.
		workerRepoForHandler = workerRepository
		credentialsRepository = repository.NewCredentialsRepository(primaryDB.Pool)
		workerOrchestrator = worker_orchestrator.New(
			workerRepository,
			credentialsRepository,
			cipherService,
			worker_orchestrator.WorkerEnvConfig{
				AppEnv:               os.Getenv("APP_ENV"),
				WorkerImage:          getenvDefault("WORKER_IMAGE", "ghcr.io/warmbly/worker:latest"),
				KafkaBootstrap:       os.Getenv("KAFKA_BOOTSTRAP_SERVERS"),
				KafkaSASLUsername:    os.Getenv("KAFKA_SASL_USERNAME"),
				KafkaSASLPassword:    os.Getenv("KAFKA_SASL_PASSWORD"),
				SchemaRegistryURL:    os.Getenv("SCHEMA_REGISTRY_URL"),
				SchemaRegistryKey:    os.Getenv("SCHEMA_REGISTRY_KEY"),
				SchemaRegistrySecret: os.Getenv("SCHEMA_REGISTRY_SECRET"),
				RedisURL:             os.Getenv("REDIS"),
				AWSRegion:            os.Getenv("AWS_REGION"),
				AWSAccessKeyID:       os.Getenv("WORKER_AWS_ACCESS_KEY_ID"),
				AWSSecretAccessKey:   os.Getenv("WORKER_AWS_SECRET_ACCESS_KEY"),
			},
			getenvDefault("WORKER_INSTALLER_PATH", "/app/scripts/install-worker.sh"),
		)

		// Releases service. Env-configurable so self-hosters can point at their
		// own repo/registry, or disable the feature entirely.
		releasesService = releases.New(
			releases.Config{
				Enabled:         getenvDefault("RELEASES_ENABLED", "true") == "true",
				GithubRepo:      getenvDefault("RELEASES_GITHUB_REPO", "warmbly/warmbly"),
				WorkerImageRepo: getenvDefault("RELEASES_WORKER_IMAGE_REPO", "ghcr.io/warmbly/warmbly/worker"),
				WebhookSecret:   os.Getenv("RELEASES_WEBHOOK_SECRET"),
				GithubToken:     os.Getenv("RELEASES_GITHUB_TOKEN"),
			},
			credentialsRepository,
			workerRepository,
			workerOrchestrator,
		)
		releasesService.RunBootCheck(ctx)

		eventsPublisher := events.NewPublisher(bus, s3, codecImpl, cipherService)

		oauth2Cfg := config.LoadOauth2(apiCfg.Hostname)
		emailService = email.NewServiceWithKafka(
			emailRepostory,
			cipherService,
			featureGateService,
			warmupService,
			eventsPublisher,
			kafkaProducer,
			cache,
			&oauth2Cfg.InboxAuthorization,
			workerAssignmentService,
		)
		// Fan out email-account lifecycle events to customer webhooks.
		emailService.WireWebhooks(webhookService)
		campaignService = campaign.NewService(campaignRepostory, taskRepository, emailRepostory, campaignLogRepository, featureGateService, streamingPublisher)
		sequenceService = sequence.NewService(sequenceRepostory)
		contactService = contact.NewService(contactRepostory, subscriptionRepository, planRepository)
		apiKeyService = apikey.NewService(cache, apiKeyRepository)
		crmService = crm.NewService(crmRepository)
		socketService = socket.NewService(cache, tokenService)
		uniboxService = unibox.NewService(cache, s3, uniboxRepository)

		// Cloud Tasks client
		cloudTasksCfg, err := cfg.LoadCloudTasksConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		tasksClient, err := gtasks.NewClient(ctx, cloudTasksCfg.QueueName, cloudTasksCfg.WebhookURL, serviceAccount, cloudTasksCfg.EmulatorHost)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// Template & email send services
		templateService = template.NewService(templateRepository)
		schedulerService := scheduler.NewSchedulerService(taskRepository, warmupRepository, campaignProgressRepository, emailRepostory, campaignRepostory)
		emailSendService = emailsend.NewService(taskRepository, emailRepostory, schedulerService, tasksClient, featureGateService)
		advancedService = advanced.NewService(
			advancedRepository,
			campaignRepostory,
			emailRepostory,
			taskRepository,
			contactRepostory,
			campaignProgressRepository,
			crmRepository,
			tasksClient,
			warmupService,
		)
		emailSender := tasks.NewEmailSender(emailRepostory, eventsPublisher)
		tasksService = tasks.NewService(
			tasksClient,
			kafkaProducer,
			nil, // AI generation client is optional for task execution
			streamingPublisher,
			eventsPublisher,
			schedulerService,
			cipherService,
			emailSender,
			featureGateService,
			warmupService,
			taskRepository,
			warmupRepository,
			warmupRoutingRepository,
			campaignProgressRepository,
			emailRepostory,
			campaignRepostory,
			contactRepostory,
			campaignLogRepository,
			advancedService,
		)

		// Admin service
		adminRepository := repository.NewAdminRepository(primaryDB.Pool)
		adminService = admin.NewService(adminRepository)

		folderService = group.NewService(folderRepostory)
		tagService = group.NewService(tagRepostory)
		categoryService = group.NewService(categoryRepostory)

		// Start trial expiration job in background
		trialExpirationJob := jobs.NewTrialExpirationJobWithDB(subscriptionRepository, primaryDB.Pool, emailNotificationService)
		trialScheduler := jobs.NewTrialExpirationScheduler(trialExpirationJob, 1*time.Hour)
		go trialScheduler.Start(ctx)

		// Danger zone: schedule + execute delayed deletions (orgs, accounts).
		dangerZoneRepository := repository.NewDangerZoneRepository(primaryDB.Pool)
		dangerZoneService = dangerzone.NewService(
			dangerZoneRepository,
			organizationRepository,
			userRepostory,
			emailNotificationService,
			os.Getenv("FRONTEND_BASE_URL"),
		)
		dangerZoneJob := jobs.NewDangerZoneJob(dangerZoneService)
		dangerZoneScheduler := jobs.NewDangerZoneScheduler(dangerZoneJob, 1*time.Hour)
		go dangerZoneScheduler.Start(ctx)

		addr = apiCfg.Hostname
		ginMode = apiCfg.GinMode
		websocketURI = apiCfg.WebsocketURI
		allowedOrigins = apiCfg.AllowedOrigins
	}

	h := &handler.Handler{
		AuthService:     authService,
		TokenService:    tokenService,
		UserService:     userService,
		EmailService:    emailService,
		CampaignService: campaignService,
		ContactService:  contactService,
		SequenceService: sequenceService,
		UniboxService:   uniboxService,

		FolderService:   folderService,
		TagService:      tagService,
		CategoryService: categoryService,

		TzService:     tzService,
		SocketService: socketService,
		TasksService:  tasksService,

		// API Keys
		APIKeyService: apiKeyService,

		// Subscription & billing
		SubscriptionService: subscriptionService,
		StripeService:       stripeService,

		// Trial & feature gates
		TrialService:            trialService,
		FeatureGateService:      featureGateService,
		WorkerAssignmentService: workerAssignmentService,

		// Organization & IAM
		OrganizationService: organizationService,

		// CRM
		CRMService: crmService,

		// Email send & templates
		TemplateService:  templateService,
		EmailSendService: emailSendService,

		// Admin
		AdminService: adminService,

		// SSH-managed worker lifecycle
		WorkerOrchestrator: workerOrchestrator,
		WorkerRepo:         workerRepoForHandler,
		CredentialsRepo:    credentialsRepository,
		ReleasesService:    releasesService,

		// Notifications
		EmailNotificationService: emailNotificationService,

		// Advanced outreach controls
		AdvancedService: advancedService,

		// Warmup health
		WarmupService:     warmupService,
		WarmupRoutingRepo: warmupRoutingRepoForHandler,
		WebhookService:    webhookServiceForHandler,

		WebsocketURI: websocketURI,

		// Object storage + direct repository handles for handlers
		// without a dedicated service layer (avatars, etc.).
		Storage:                  s3ForHandler,
		EncryptedKeys:            encryptedKeys,
		UserRepo:                 userRepoForHandler,
		OrgRepo:                  organizationRepoForHandler,
		StorageBackendRepo:       storageBackendRepo,
		CloudCredentialRepo:      cloudCredentialRepo,
		ProvisioningTemplateRepo: provisioningTemplateRepo,
		ProvisioningJobRepo:      provisioningJobRepo,
		ProvisioningPolicyRepo:   provisioningPolicyRepo,

		// Danger zone
		DangerZoneService: dangerZoneService,

		// Audit logs aren't persisted yet; install a no-op so the
		// many h.AuditService.LogAction sites don't panic on a nil
		// interface. Swap for audit.NewService(repo) when wiring the
		// real repository.
		AuditService: audit.NewNoOpService(),
	}

	m := &middleware.Handler{
		TokenService:        tokenService,
		APIKeyService:       apiKeyService,
		OrganizationService: organizationService,
	}

	oidcH := &middleware.OidcHandler{
		ServiceAccount: serviceAccount,
		KeySet:         keySet,
		AppEnv:         os.Getenv("APP_ENV"),
	}

	sentry.CaptureMessage("Starting the backend on " + addr)

	router := api.Run(h, m, oidcH, addr, ginMode, allowedOrigins)

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	log.Println("Backend started on", addr)

	// Wait for interrupt signal for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down backend...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Backend stopped")
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
