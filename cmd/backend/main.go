package main

import (
	"context"
	"fmt"
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
	"github.com/warmbly/warmbly/internal/app/adminoutreach"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/crm"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/app/dangerzone"
	"github.com/warmbly/warmbly/internal/app/discount"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	emailverifyapp "github.com/warmbly/warmbly/internal/app/emailverify"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/fleet"
	"github.com/warmbly/warmbly/internal/app/group"
	idempotencyapp "github.com/warmbly/warmbly/internal/app/idempotency"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/passkey"
	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/app/provisioning"
	"github.com/warmbly/warmbly/internal/app/ratelimit"
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
	"github.com/warmbly/warmbly/internal/app/warmupcontent"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/app/worker_orchestrator"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/cloudprovider"
	"github.com/warmbly/warmbly/internal/infrastructure/cloudprovider/hetzner"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
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
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"github.com/warmbly/warmbly/internal/pkg/generation"
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
	var analyticsService analytics.AnalyticsService
	var rateLimitService ratelimit.RateLimitService
	var sequenceService sequence.SequenceService
	var contactService contact.ContactService
	var socketService socket.SocketService
	var uniboxService unibox.UniboxService
	var cipherService cipher.CipherService
	var passkeyService passkey.Service
	var encryptedKeys encryptedkeys.Store
	var storageBackendRepo repository.StorageBackendRepository
	var cloudCredentialRepo repository.CloudCredentialRepository
	var provisioningTemplateRepo repository.ProvisioningTemplateRepository
	var provisioningJobRepo repository.ProvisioningJobRepository
	var provisioningPolicyRepo repository.ProvisioningPolicyRepository
	var tasksService tasks.TasksService
	var advancedService advanced.Service
	var warmupContentRepo repository.WarmupContentRepository
	var warmupContentService warmupcontent.Service
	var emailVerifyService emailverifyapp.Service
	var placementRepository repository.PlacementRepository
	var placementService placement.Service

	var folderService group.GroupService
	var tagService group.GroupService
	var categoryService group.GroupService
	var crmService crm.CRMService
	var apiKeyService apikey.APIKeyService
	var idempotencyService idempotencyapp.Service

	// New services for trial, feature gates, and worker assignment
	var trialService trial.TrialService
	var featureGateService feature.FeatureGateService
	var workerAssignmentService worker.WorkerAssignmentService
	var subscriptionService subscription.SubscriptionService
	var stripeService stripe.StripeService
	var discountService discount.DiscountService
	var organizationService organization.OrganizationService

	// Email send & templates
	var templateService template.TemplateService
	var emailSendService emailsend.EmailSendService

	// Admin
	var adminService admin.AdminService
	var adminOutreachService adminoutreach.Service
	var dailyThrottleService dailythrottle.Service

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

	// Organization-wide audit trail
	var auditService audit.AuditService

	// Pub/Sub for realtime streaming
	var streamingPublisher *pubsub.StreamingPublisher

	// Surfaced into the handler for avatar uploads and other direct
	// repository / object-storage needs. Declared up here so they
	// survive the config block where they're initialized.
	var s3ForHandler *storage.Client
	var emailMessageMapForHandler repository.EmailMessageMapRepository
	var userRepoForHandler repository.UserRepository
	var organizationRepoForHandler repository.OrganizationRepository
	var warmupRoutingRepoForHandler repository.WarmupRoutingRepository
	var webhookServiceForHandler webhook.Service
	var integrationServiceForHandler integration.Service
	var contactRepoForHandler repository.ContactRepository

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

		// AWS config for services that need it (KMS, S3)
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
				// geo.New returns a nil client on error; fall back to a usable,
				// geo-disabled client so downstream callers never deref nil.
				geoloc, _ = geo.New("")
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
		webauthnRepository := repository.NewWebAuthnRepository(primaryDB)
		emailRepostory := repository.NewEmailRepostory(primaryDB)
		campaignRepostory := repository.NewCampaignRepostory(primaryDB)
		sequenceRepostory := repository.NewSequenceRepostory(primaryDB)
		contactRepostory := repository.NewContactRepostory(primaryDB)
		uniboxRepository := repository.NewUniboxRepository(primaryDB)
		encryptedKeys, err = encryptedkeys.FromEnv(
			encryptedkeys.Deps{DB: primaryDB},
			"postgres",
		)
		emailMessageMapForHandler = repository.NewEmailMessageMapRepository(primaryDB)
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

		// Admin + discount management. Constructed before the Stripe service:
		// the Stripe service depends on the discount service (to validate codes
		// and record redemptions at checkout), and the discount service audits
		// management actions through the admin service.
		adminRepository := repository.NewAdminRepository(primaryDB.Pool)
		adminService = admin.NewService(adminRepository)
		discountCodeRepository := repository.NewDiscountCodeRepository(primaryDB.Pool)
		discountRedemptionRepository := repository.NewDiscountRedemptionRepository(primaryDB.Pool)
		discountService = discount.NewService(discountCodeRepository, discountRedemptionRepository, planRepository, adminService)
		workerRepository := repository.NewWorkerRepository(primaryDB.Pool)
		organizationRepository := repository.NewOrganizationRepository(primaryDB.Pool)
		organizationRepoForHandler = organizationRepository
		taskRepository := repository.NewTaskRepository(primaryDB.Pool)
		apiKeyRepository := repository.NewAPIKeyRepository(primaryDB)
		idempotencyService = idempotencyapp.NewService(primaryDB.Pool)
		crmRepository := repository.NewCRMRepository(primaryDB.Pool)
		advancedRepository := repository.NewAdvancedOutreachRepository(primaryDB.Pool)
		templateRepository := repository.NewTemplateRepository(primaryDB.Pool)
		warmupRepository := repository.NewWarmupRepository(primaryDB.Pool)
		warmupRoutingRepository := repository.NewWarmupRoutingRepository(primaryDB.Pool)
		warmupRoutingRepoForHandler = warmupRoutingRepository

		// Warmup content bank + offline AI generator. The generation client is
		// optional: without OPENAI_API_KEY the live send path simply keeps using
		// the static library and admin generation returns "not configured".
		warmupContentRepo = repository.NewWarmupContentRepository(primaryDB.Pool)
		var generationClient *generation.GenerationClient
		if openaiKey := cfg.GetSecretOptional(ctx, "OPENAI_API_KEY", "openai_api_key", ""); openaiKey != "" {
			generationClient = generation.NewClient(openaiKey)
		}
		warmupContentService = warmupcontent.NewService(warmupContentRepo, generationClient)
		webhookRepository := repository.NewWebhookRepository(primaryDB.Pool)
		webhookService := webhook.NewService(webhookRepository)
		webhookServiceForHandler = webhookService

		integrationRepository := repository.NewIntegrationRepository(primaryDB.Pool)
		// integrationServiceForHandler is constructed after cipherService below —
		// OAuth/secret sealing depends on the envelope-encryption service.
		contactRepoForHandler = contactRepostory

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
		// dailyThrottleService needs the cache that's constructed
		// earlier in main; instantiate up here so org create can use it.
		if dailyThrottleService == nil {
			dailyThrottleService = dailythrottle.NewService(cache)
		}
		organizationService = organization.NewService(organizationRepository, subscriptionRepository, userRepostory, dailyThrottleService)

		// Load Stripe config and initialize service
		stripeCfg, err := cfg.LoadStripeConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}
		stripeService = stripe.NewService(stripeCfg, subscriptionRepository, planRepository, workerAssignmentService, discountService)

		tokenService = token.NewService(primaryDB, tokenRepostory, cache, geoloc, authCfg.AuthSecret)
		userService = user.NewService(userRepostory, cache)

		// Organization-wide audit trail (who did what, when, from where).
		auditRepository := repository.NewAuditRepository(primaryDB.Pool)
		auditService = audit.NewService(auditRepository, streamingPublisher)

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
		var passkeyErr error
		passkeyService, passkeyErr = passkey.New(passkey.Deps{
			Repo:          webauthnRepository,
			UserRepo:      userRepostory,
			TokenService:  tokenService,
			Cache:         cache,
			RPID:          authCfg.WebAuthnRPID,
			RPDisplayName: authCfg.WebAuthnRPDisplayName,
			RPOrigins:     authCfg.WebAuthnRPOrigins,
		})
		if passkeyErr != nil {
			sentry.CaptureException(passkeyErr)
			log.Fatal(passkeyErr)
		}
		cipherService = cipher.NewService(kms, cache, encryptedKeys)

		// Third-party integrations: OAuth connect flows + encrypted token
		// storage (sealed with the connecting user's DEK) + event-driven actions.
		integrationServiceForHandler = integration.NewService(integrationRepository, cipherService, integration.NewOAuthManager())
		// Fan platform events (replies, bounces, warmup health, booked meetings)
		// out to integration actions alongside customer webhooks.
		webhookService.WireDispatchSink(integrationServiceForHandler.DispatchAny)

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

		// Provisioning runner. Drives provisioning_jobs rows to completion —
		// without it a job created from the admin UI sits in "pending" forever.
		//
		// Real Hetzner calls only happen when PROVISIONING_DRY_RUN=false. A real
		// SSH installer adapter (over worker_orchestrator) is not wired yet, so
		// until it is we force dry-run: real-mode would otherwise create servers
		// it could not provision, leaving orphaned, billed machines. Dry-run runs
		// the full state machine against a simulated provider so the admin flow
		// works end-to-end in dev without spending money.
		if getenvDefault("PROVISIONING_RUNNER_ENABLED", "true") == "true" {
			provDryRun := getenvDefault("PROVISIONING_DRY_RUN", "true") != "false"
			if !provDryRun {
				log.Printf("PROVISIONING_DRY_RUN=false but no real installer is wired; forcing dry-run to avoid orphaned servers")
				provDryRun = true
			}
			credRepoForResolver := cloudCredentialRepo
			provService := &provisioning.Service{
				Jobs:      provisioningJobRepo,
				Installer: &provisioning.StubInstaller{},
				ProviderResolver: func(rctx context.Context, job *repository.ProvisioningJob) (cloudprovider.Provider, error) {
					if provDryRun {
						return provisioning.DryRunProvider{}, nil
					}
					if credRepoForResolver == nil {
						return nil, fmt.Errorf("no cloud credential repo configured")
					}
					cred, err := credRepoForResolver.GetByProvider(rctx, job.Provider)
					if err != nil {
						return nil, err
					}
					if cred == nil {
						return nil, fmt.Errorf("no cloud credential for provider %q", job.Provider)
					}
					switch cred.Provider {
					case "hetzner":
						return hetzner.New(cred.EncryptedToken)
					default:
						return nil, fmt.Errorf("unsupported provider %q", cred.Provider)
					}
				},
			}
			go (&provisioning.Runner{
				Jobs:   provisioningJobRepo,
				Svc:    provService,
				DryRun: provDryRun,
			}).Run(ctx)
		}

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
				AppEnv:                   os.Getenv("APP_ENV"),
				WorkerImage:              getenvDefault("WORKER_IMAGE", "ghcr.io/warmbly/worker:latest"),
				KafkaBootstrap:           os.Getenv("KAFKA_BOOTSTRAP_SERVERS"),
				KafkaSASLUsername:        os.Getenv("KAFKA_SASL_USERNAME"),
				KafkaSASLPassword:        os.Getenv("KAFKA_SASL_PASSWORD"),
				SchemaRegistryURL:        os.Getenv("SCHEMA_REGISTRY_URL"),
				SchemaRegistryKey:        os.Getenv("SCHEMA_REGISTRY_KEY"),
				SchemaRegistrySecret:     os.Getenv("SCHEMA_REGISTRY_SECRET"),
				RedisURL:                 os.Getenv("REDIS"),
				AWSRegion:                os.Getenv("AWS_REGION"),
				AWSAccessKeyID:           os.Getenv("WORKER_AWS_ACCESS_KEY_ID"),
				AWSSecretAccessKey:       os.Getenv("WORKER_AWS_SECRET_ACCESS_KEY"),
				EncryptedKeysBackendURL:  os.Getenv("ENCRYPTED_KEYS_BACKEND_URL"),
				EncryptedKeysWorkerToken: os.Getenv("INTERNAL_API_TOKEN"),
				EventBusProvider:         os.Getenv("EVENTBUS_PROVIDER"),
				NATSURL:                  os.Getenv("NATS_URL"),
				CodecProvider:            os.Getenv("CODEC_PROVIDER"),
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
			streamingPublisher,
		)
		// Fan out email-account lifecycle events to customer webhooks.
		emailService.WireWebhooks(webhookService)
		// Same wire-after-construct pattern for the daily throttle —
		// only the prod backend has a real cache; jobs / tests build
		// emailService without one.
		emailService.WireThrottle(dailyThrottleService)
		analyticsRepository := repository.NewAnalyticsRepository(primaryDB)
		emailAccountErrorRepository := repository.NewEmailAccountErrorRepository(primaryDB)
		analyticsService = analytics.NewService(analyticsRepository, emailRepostory, campaignRepostory, emailAccountErrorRepository, warmupRepository)

		rateLimitRepository := repository.NewRateLimitRepository(primaryDB)
		rateLimitService = ratelimit.NewService(cache, rateLimitRepository)
		sequenceService = sequence.NewService(sequenceRepostory)
		contactService = contact.NewService(contactRepostory, subscriptionRepository, planRepository, streamingPublisher)
		apiKeyService = apikey.NewService(cache, apiKeyRepository)
		crmService = crm.NewService(crmRepository)
		socketService = socket.NewService(cache, tokenService)

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
		campaignService = campaign.NewService(campaignRepostory, taskRepository, emailRepostory, campaignLogRepository, featureGateService, dailyThrottleService, schedulerService, tasksClient, streamingPublisher)
		emailSendService = emailsend.NewService(taskRepository, emailRepostory, userRepostory, schedulerService, tasksClient, featureGateService, dailyThrottleService)
		// uniboxService is constructed here (rather than alongside the
		// other service constructors above) because cancel-scheduled
		// needs the Cloud Tasks client for best-effort DeleteTask, and
		// tasksClient isn't initialised until the Cloud Tasks config
		// block runs.
		uniboxService = unibox.NewService(cache, s3, uniboxRepository, taskRepository, tasksClient)
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
		// Fan reply + bounce events from the advanced-outreach brain out to
		// customer webhooks AND third-party integration actions (Slack / CRM).
		advancedService.WireDispatcher(webhookService)
		emailSender := tasks.NewEmailSender(emailRepostory, eventsPublisher)
		tasksService = tasks.NewService(
			tasksClient,
			kafkaProducer,
			generationClient,
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
			warmupContentRepo,
			campaignProgressRepository,
			emailRepostory,
			campaignRepostory,
			contactRepostory,
			campaignLogRepository,
			advancedService,
		)

		// Admin outreach composer — sends from the platform mailer
		// (SES/SMTP) with a configurable Reply-To, audits every send.
		adminOutreachRepo := repository.NewAdminOutreachRepository(primaryDB.Pool)
		adminOutreachService = adminoutreach.NewService(
			adminOutreachRepo,
			userRepostory,
			organizationRepository,
			emailNotificationService,
		)

		folderService = group.NewService(folderRepostory)
		tagService = group.NewService(tagRepostory)
		categoryService = group.NewService(categoryRepostory)

		// Start trial expiration job in background
		trialExpirationJob := jobs.NewTrialExpirationJobWithDB(subscriptionRepository, primaryDB.Pool, emailNotificationService)
		trialScheduler := jobs.NewTrialExpirationScheduler(trialExpirationJob, 1*time.Hour)
		go trialScheduler.Start(ctx)

		// Warmup reconciler: seed/repair warmup chains for mailboxes that are
		// warming or backing a live campaign (the health-check lane). This is
		// the bootstrap — enabling warmup or starting a campaign doesn't itself
		// enqueue the first warmup task.
		go tasksService.StartWarmupReconciler(ctx, 10*time.Minute)

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

		// Prune audit entries past the retention window (90 days). Bounding the
		// trail's age also bounds how long PII is retained. auditRepository is
		// constructed earlier (before authService).
		auditRetentionJob := jobs.NewAuditRetentionJob(auditRepository, 90*24*time.Hour)
		auditRetentionScheduler := jobs.NewAuditRetentionScheduler(auditRetentionJob, 6*time.Hour)
		go auditRetentionScheduler.Start(ctx)

		// Warmup content generator: tops the AI thread bank up toward the
		// admin-configured per-pool/segment targets. The internal cadence gate
		// honours the admin's cadence_hours; it no-ops when generation is
		// disabled or unconfigured.
		warmupGenerationJob := jobs.NewWarmupGenerationJob(warmupContentService, warmupContentRepo)
		warmupGenerationScheduler := jobs.NewWarmupGenerationScheduler(warmupGenerationJob, 30*time.Minute)
		go warmupGenerationScheduler.Start(ctx)

		// Warmup batch poller: reconciles in-flight OpenAI Batch API generation
		// jobs (~50% cheaper, async up to 24h), ingesting completed batches into
		// the content bank and marking failed/expired/cancelled ones. No-ops when
		// generation is unconfigured or there are no active batch jobs.
		warmupBatchPoller := jobs.NewWarmupBatchPoller(warmupContentService, 5*time.Minute)
		go warmupBatchPoller.Start(ctx)

		// Pre-send email verification: verify a capped batch of not-yet-checked
		// contacts each tick so hard-bouncing addresses are dropped before any
		// worker sends. CONTROL-PLANE ONLY — the SMTP RCPT probe dials remote MX
		// on :25 from this backend host (a non-sending IP), never a worker.
		emailVerifier := emailverify.New(emailverify.Config{
			HeloHost: os.Getenv("EMAIL_VERIFY_HELO_HOST"), // e.g. verify.warmbly.com
			MailFrom: os.Getenv("EMAIL_VERIFY_MAIL_FROM"), // e.g. verify@warmbly.com
		})
		emailVerifyService = emailverifyapp.NewService(contactRepostory, emailVerifier)
		emailVerificationJob := jobs.NewEmailVerificationJob(emailVerifyService, 100)
		emailVerificationScheduler := jobs.NewEmailVerificationScheduler(emailVerificationJob, 15*time.Minute)
		go emailVerificationScheduler.Start(ctx)

		// Seed inbox-placement testing: send a tokenized copy of a template
		// through a real sender to the seed panel, then classify where it landed
		// by looking the token up in each seed's synced unibox entries.
		placementRepository = repository.NewPlacementRepository(primaryDB)
		placementService = placement.NewService(placementRepository, emailRepostory, emailSender)
		placementPoller := jobs.NewPlacementPoller(placementService, 2*time.Minute)
		go placementPoller.Start(ctx)

		addr = apiCfg.Hostname
		ginMode = apiCfg.GinMode
		websocketURI = apiCfg.WebsocketURI
		allowedOrigins = apiCfg.AllowedOrigins
	}

	h := &handler.Handler{
		AuthService:      authService,
		TokenService:     tokenService,
		PasskeyService:   passkeyService,
		UserService:      userService,
		EmailService:     emailService,
		CampaignService:  campaignService,
		AnalyticsService: analyticsService,
		RateLimitService: rateLimitService,
		ContactService:   contactService,
		SequenceService:  sequenceService,
		UniboxService:    uniboxService,

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
		DiscountService:     discountService,

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
		AdminService:         adminService,
		AdminOutreachService: adminOutreachService,

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

		// Warmup content bank + offline AI generator
		WarmupContentRepo:    warmupContentRepo,
		WarmupContentService: warmupContentService,

		// Pre-send email verification
		EmailVerifyService: emailVerifyService,

		// Seed inbox-placement testing
		PlacementRepo:    placementRepository,
		PlacementService: placementService,

		// Third-party integrations
		IntegrationService: integrationServiceForHandler,
		ContactRepo:        contactRepoForHandler,

		WebsocketURI: websocketURI,

		// Object storage + direct repository handles for handlers
		// without a dedicated service layer (avatars, etc.).
		Storage:                  s3ForHandler,
		EncryptedKeys:            encryptedKeys,
		EmailMessageMap:          emailMessageMapForHandler,
		UserRepo:                 userRepoForHandler,
		OrgRepo:                  organizationRepoForHandler,
		StorageBackendRepo:       storageBackendRepo,
		CloudCredentialRepo:      cloudCredentialRepo,
		ProvisioningTemplateRepo: provisioningTemplateRepo,
		ProvisioningJobRepo:      provisioningJobRepo,
		ProvisioningPolicyRepo:   provisioningPolicyRepo,

		// Danger zone
		DangerZoneService: dangerZoneService,

		// Organization-wide audit trail, backed by Postgres. The no-op
		// fallback (audit.NewNoOpService) remains for entrypoints without
		// a database.
		AuditService: auditService,
	}

	m := &middleware.Handler{
		TokenService:        tokenService,
		APIKeyService:       apiKeyService,
		IdempotencyService:  idempotencyService,
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
