package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/meszmate/apple-go"
	"github.com/meszmate/google-go"
	"github.com/warmbly/warmbly/internal/api"
	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/admin"
	"github.com/warmbly/warmbly/internal/app/adminoutreach"
	"github.com/warmbly/warmbly/internal/app/advanced"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/app/advisor"
	"github.com/warmbly/warmbly/internal/app/aiagent"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/auth"
	behaviorapp "github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/app/bootstrap"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/compose"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/creditwatch"
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
	"github.com/warmbly/warmbly/internal/app/guardrail"
	idempotencyapp "github.com/warmbly/warmbly/internal/app/idempotency"
	"github.com/warmbly/warmbly/internal/app/inboxagent"
	"github.com/warmbly/warmbly/internal/app/instancecheck"
	"github.com/warmbly/warmbly/internal/app/instanceconfig"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/app/leadsync"
	"github.com/warmbly/warmbly/internal/app/mcp"
	"github.com/warmbly/warmbly/internal/app/nativeactions"
	"github.com/warmbly/warmbly/internal/app/notification"
	"github.com/warmbly/warmbly/internal/app/oauth"
	"github.com/warmbly/warmbly/internal/app/oidcauth"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/app/passkey"
	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/app/provisioning"
	"github.com/warmbly/warmbly/internal/app/ratelimit"
	"github.com/warmbly/warmbly/internal/app/referral"
	"github.com/warmbly/warmbly/internal/app/releases"
	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/app/research"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/settings"
	"github.com/warmbly/warmbly/internal/app/skills"
	"github.com/warmbly/warmbly/internal/app/socket"
	"github.com/warmbly/warmbly/internal/app/stripe"
	"github.com/warmbly/warmbly/internal/app/subscription"
	"github.com/warmbly/warmbly/internal/app/sysstatus"
	"github.com/warmbly/warmbly/internal/app/team"
	"github.com/warmbly/warmbly/internal/app/template"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/twofa"
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
	"github.com/warmbly/warmbly/internal/infrastructure/apns"
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
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	"github.com/warmbly/warmbly/internal/tasksched"
)

func main() {
	// Before anything else: refuse to run a networked deployment on the
	// published default secrets. They are in this repository, so they protect
	// nothing, and a forged token signed with the default AUTH_SECRET is
	// accepted by the realtime service.
	insecureDefaultsInUse := checkSecrets()

	var addr string
	var ginMode string
	var websocketURI string
	var allowedOrigins []string
	var systemChecker *sysstatus.Checker

	var tzService tz.TzService

	var serviceAccount string
	var keySet keyfunc.Keyfunc

	var tokenService token.TokenService
	var authService auth.AuthService
	var externalAuthProviders models.ExternalAuthProviders
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
	var creditRepository repository.CreditRepository
	var aiSettingsRepository repository.AISettingsRepository
	var creditService credits.CreditService
	var aiDraftRepo repository.AIDraftRepository
	var writingGenerator generation.WritingGenerator
	var aiProvider generation.Provider
	var aiSearch generation.SearchClient
	var aiToolRegistry *aitools.Registry
	var aiAgentService aiagent.Service
	var researchService research.Service
	var skillsService skills.Service
	var mcpService mcp.Service
	var emailVerifyService emailverifyapp.Service
	var placementRepository repository.PlacementRepository
	var placementService placement.Service
	var advisorRepository repository.AdvisorRepository
	var advisorService advisor.Service

	var folderService group.GroupService
	var tagService group.GroupService
	var categoryService group.GroupService
	var crmService crm.CRMService
	var teamService team.TeamService
	var apiKeyService apikey.APIKeyService
	var idempotencyService idempotencyapp.Service

	// New services for trial, feature gates, and worker assignment
	var trialService trial.TrialService
	var featureGateService feature.FeatureGateService
	var workerAssignmentService worker.WorkerAssignmentService
	var subscriptionService subscription.SubscriptionService
	var stripeService stripe.StripeService
	var discountService discount.DiscountService
	var referralService referral.Service
	var organizationService organization.OrganizationService

	// Email send & templates
	var templateService template.TemplateService
	var emailSendService emailsend.EmailSendService
	var composeService compose.Service

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
	// mailTransport is the same sender plus its metadata, kept so the auth
	// config endpoint and admin diagnostics can report what mail is doing.
	var mailTransport *notify.Transport
	// Deployment auth facts resolved during boot and served by /auth/config.
	var authPolicy *config.AuthPolicy
	var passkeysUsable bool
	var googleWebSignIn, appleWebSignIn bool
	var bootstrapService *bootstrap.Service
	// oidcLogin is nil unless OIDC_ISSUER_URL is configured.
	var oidcLogin *oidcauth.Service
	// oidcDiscoveryErr keeps a boot-time discovery failure reportable, since it
	// otherwise only ever appeared once in the log.
	var oidcDiscoveryErr string
	// authCache is the same Redis client the services use, hoisted so the
	// middleware handler can reach it for the pre-login per-IP limiter.
	var authCache *cache.Cache

	// Warmup
	var warmupService warmupapp.Service

	// Per-mailbox human sending behaviour (rolled workday, daily/hourly
	// ceilings, send spacing) — consumed by the schedulers and the dashboard.
	var behaviorService behaviorapp.Service

	// Danger zone (delayed deletions)
	var dangerZoneService dangerzone.Service

	// Workspace archives (export/import between instances)
	var orgTransferService orgtransfer.Service

	// Organization-wide audit trail
	var auditService audit.AuditService

	// Pub/Sub for realtime streaming
	var streamingPublisher *pubsub.StreamingPublisher

	// Surfaced into the handler for avatar uploads and other direct
	// repository / object-storage needs. Declared up here so they
	// survive the config block where they're initialized.
	var s3ForHandler storage.Store
	var emailMessageMapForHandler repository.EmailMessageMapRepository
	var emailSyncStateRepository repository.EmailSyncStateRepository
	var trackedLinkRepository repository.TrackedLinkRepository
	// instanceSettings and the health registry are built after the handler
	// dependencies, so the pool is hoisted out of the connection block.
	var instanceSettings instancesettings.Service
	var instanceChecksDB *pgxpool.Pool
	var userRepoForHandler repository.UserRepository
	var organizationRepoForHandler repository.OrganizationRepository
	var warmupRoutingRepoForHandler repository.WarmupRoutingRepository
	var webhookServiceForHandler webhook.Service
	var integrationServiceForHandler integration.Service
	var oauthService *oauth.Service
	var notificationService notification.Service
	var twofaService twofa.Service
	var contactRepoForHandler repository.ContactRepository
	var attachmentRepoForHandler repository.AttachmentRepository
	var leadSyncServiceForHandler leadsync.Service

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

		// The Google service account + OIDC key set only authenticate GCP Cloud
		// Tasks webhook callbacks. Under the default local dispatcher there is no
		// webhook, so skip both — a self-host boot needs no GCP identity and no
		// outbound call to Google's cert endpoint.
		if config.TasksProvider() == "gcloud" {
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
		}

		apiCfg, err := cfg.LoadApiConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// AWS SDK config, loaded only when an AWS-backed provider is selected
		// (KMS_PROVIDER=aws or BLOB_PROVIDER=s3). A fully-local self-host
		// (local KMS + filesystem blobs) skips this and needs no AWS_REGION or
		// credentials at all.
		var awscfg aws.Config
		if config.AWSNeeded() {
			awscfg, err = awsconf.LoadDefaultConfig(ctx)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
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

		// GeoIP is optional everywhere. It only labels session and audit records
		// with a city, so a missing database is a cosmetic loss, not a reason to
		// refuse to start: requiring a MaxMind licence to run APP_ENV=prod made
		// self-hosting depend on an account nobody asked for.
		var geoloc *geo.Client
		geoloc, err = geo.New(geoPath)
		if err != nil {
			log.Printf("GeoIP database not found at %s; location lookups are disabled.", geoPath)
			// geo.New returns a nil client on error; fall back to a usable,
			// geo-disabled client so downstream callers never deref nil.
			geoloc, _ = geo.New("")
		}

		s3, err := storage.NewFromEnv(ctx, awscfg, "main")
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

		// Realtime event transport, chosen by PUBSUB_ENABLED — the SAME flag the
		// Elixir realtime service reads — so the two sides can never split-brain
		// (publisher on Pub/Sub while the subscriber listens on Redis = all events
		// dropped). PUBSUB_ENABLED=true => Google Pub/Sub (prod); anything else =>
		// Redis bridge (local dev / non-GCP). Exactly one transport is active, so
		// events are never delivered twice.
		if os.Getenv("PUBSUB_ENABLED") == "true" {
			gcpProjectID := os.Getenv("GCP_PROJECT_ID")
			if gcpProjectID == "" {
				log.Fatal("PUBSUB_ENABLED=true requires GCP_PROJECT_ID")
			}
			pubsubClient, err := pubsub.NewClient(ctx, gcpProjectID)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal("Failed to initialize Pub/Sub client: ", err)
			}
			// Create the realtime topics + "<topic>-sub" subscriptions if missing,
			// so the Elixir Broadway consumers always have a subscription to read.
			if err := pubsubClient.EnsureRealtimeTopology(ctx); err != nil {
				sentry.CaptureException(err)
				log.Fatal("Failed to provision Pub/Sub topics/subscriptions: ", err)
			}
			streamingPublisher = pubsub.NewStreamingPublisher(pubsubClient)
			log.Println("Realtime events published to Google Pub/Sub")
		}
		if streamingPublisher == nil {
			streamingPublisher = pubsub.NewStreamingPublisher(pubsub.NewRedisBus(cache.Client, ""))
			log.Println("Realtime events bridged over Redis (Pub/Sub disabled)")
		}

		emailCfg, err := cfg.LoadEmailConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		mailTransport, err = notify.NewTransport(ctx, cfg, emailCfg.EmailName, emailCfg.EmailAddress)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}
		emailNotificationService = mailTransport
		// Dial the relay once at boot. A broken relay used to surface only as a
		// 500 on the login screen, which is the one screen the operator needs
		// when they cannot log in.
		if perr := mailTransport.Preflight(ctx); perr != nil {
			log.Printf("Warning: platform mail preflight FAILED (%s): %v", mailTransport.Description, perr)
			log.Printf("Warning: login codes, password resets and invitations will not be delivered. Set MAIL_TRANSPORT=log to read them from these logs instead.")
		} else {
			log.Printf("Platform mail transport: %s", mailTransport.Description)
		}
		if !mailTransport.Delivers {
			log.Printf("Warning: MAIL_TRANSPORT=log — every platform email, including login codes, is printed here and never delivered. Do not use this on a deployment other people can reach.")
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

		// Apple Sign in is optional. Skip it entirely when unconfigured (a
		// self-host without Apple creds); only warn — never fatal — when creds
		// are present but init fails, so Apple simply stays unavailable.
		var appleAuthClient apple.AppleAuth
		if authCfg.AppleAppID != "" || authCfg.AppleKeySecret != "" {
			appleAuthInstance, appleErr := apple.NewB64(
				authCfg.AppleAppID,
				authCfg.AppleTeamID,
				authCfg.AppleKeyID,
				authCfg.AppleKeySecret,
			)
			if appleErr != nil {
				log.Printf("Warning: Apple auth initialization failed; Apple sign-in disabled: %v", appleErr)
			} else {
				appleAuthClient = appleAuthInstance
			}
		}

		// Event bus + codec. Kafka bootstrap/SASL is only loaded for
		// EVENTBUS_PROVIDER=kafka; the default NATS path needs none of it. The
		// codec (codec.FromEnv) owns serialization end-to-end — Avro pulls
		// SCHEMA_REGISTRY_URL from env, JSON needs nothing — so a NATS + JSON
		// self-host requires no Kafka or Schema Registry config.
		var kafkaBootstrapServers string
		var kafkaSaslConfig *kafka.SASLConfig
		if config.EventBusProvider() == "kafka" {
			kafkaBootstrapServers, err = cfg.LoadKafkaBootstrapServers(ctx)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
			kafkaSaslConfig, err = cfg.LoadKafkaConfigSasl(ctx)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
		}

		codecImpl, err := codec.FromEnv()
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		bus, err := eventbus.FromEnv(kafkaBootstrapServers, kafkaSaslConfig)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		// The bypass token must be set explicitly. It used to fall back to a
		// hardcoded literal whenever APP_ENV was "dev", which is the shipped
		// default in both compose and env.example, so any deployment that
		// turned captcha on while leaving APP_ENV alone had a universal,
		// publicly known bypass on login, registration and password reset.
		turnstileBypassToken := ""
		if cfg.Env == "dev" {
			turnstileBypassToken = authCfg.TurnstileBypass
			if turnstileBypassToken != "" && authCfg.TurnstileSecret != "" {
				log.Printf("Warning: TURNSTILE_BYPASS_TOKEN is set alongside a real TURNSTILE_SECRET. Anyone who learns the token skips the captcha entirely.")
			}
		}

		// Captcha is off when CAPTCHA_PROVIDER=none or no TURNSTILE_SECRET is set
		// (a self-host that never configured Cloudflare), so auth endpoints work
		// without a challenge instead of rejecting every request.
		captcha := captcha.NewTurnstileFromConfig(captcha.TurnstileConfig{
			Secret:      authCfg.TurnstileSecret,
			BypassToken: turnstileBypassToken,
			Disabled:    config.CaptchaProvider() == "none",
		})

		userRepostory := repository.NewUserRepostory(primaryDB, kms)
		userRepoForHandler = userRepostory
		authRepostory := repository.NewAuthRepostory(primaryDB)
		tokenRepostory := repository.NewTokenRepostory(primaryDB)
		webauthnRepository := repository.NewWebAuthnRepository(primaryDB)
		credEncrypter, cerr := encrypt.FromEnv()
		if cerr != nil {
			sentry.CaptureException(cerr)
			log.Fatal("Invalid CREDENTIALS_ENCRYPTION_KEY: ", cerr)
		}
		emailRepostory := repository.NewEmailRepostory(primaryDB, credEncrypter)
		campaignRepostory := repository.NewCampaignRepostory(primaryDB)
		sequenceRepostory := repository.NewSequenceRepostory(primaryDB)
		contactRepostory := repository.NewContactRepostory(primaryDB)
		attachmentRepoForHandler = repository.NewAttachmentRepository(primaryDB)
		uniboxRepository := repository.NewUniboxRepository(primaryDB)
		encryptedKeys, err = encryptedkeys.FromEnv(
			encryptedkeys.Deps{DB: primaryDB},
			"postgres",
		)
		emailMessageMapForHandler = repository.NewEmailMessageMapRepository(primaryDB)
		trackedLinkRepository = repository.NewTrackedLinkRepository(primaryDB.Pool)
		instanceChecksDB = primaryDB.Pool
		instanceSettings = instancesettings.NewService(instancesettings.NewStore(primaryDB.Pool))
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

		warmupContentRepo = repository.NewWarmupContentRepository(primaryDB.Pool)

		// AI provider layer. AI_PROVIDER picks a preset (openai/openrouter/groq/
		// ollama/anthropic/custom) that fills in the base URL + free default; the
		// AI_* vars supply the key/model/endpoint. Pluggable web search
		// (Serper/SearXNG) backs search_web.
		aiProviderName := strings.ToLower(strings.TrimSpace(cfg.GetStringOptional(ctx, "AI_PROVIDER", "ai_provider", "")))
		aiKey := cfg.GetSecretOptional(ctx, "AI_API_KEY", "ai_api_key", "")
		aiSearch = generation.NewSearchClient(
			cfg.GetStringOptional(ctx, "SEARCH_PROVIDER", "search/provider", ""),
			cfg.GetStringOptional(ctx, "SEARCH_API_URL", "search/api_url", ""),
			cfg.GetSecretOptional(ctx, "SEARCH_API_KEY", "search/api_key", ""),
		)
		if cfgAI, rerr := generation.Resolve(generation.ProviderSettings{
			Provider:   aiProviderName,
			APIKey:     aiKey,
			BaseURL:    cfg.GetStringOptional(ctx, "AI_BASE_URL", "ai_base_url", ""),
			Model:      cfg.GetStringOptional(ctx, "AI_MODEL", "ai_model", ""),
			ModelTrial: cfg.GetStringOptional(ctx, "AI_MODEL_TRIAL", "ai_model_trial", ""),
			ModelPaid:  cfg.GetStringOptional(ctx, "AI_MODEL_PAID", "ai_model_paid", ""),
			Free:       cfg.GetBoolPtr(ctx, "AI_FREE", "ai_free"),
			Search:     aiSearch,
		}); rerr != nil {
			log.Printf("AI provider misconfigured, AI features disabled: %v", rerr)
		} else if provider, perr := generation.NewProvider(cfgAI); perr == nil {
			aiProvider = provider
		}

		// Warmup content bank + offline AI generator. The generator rides OpenAI's
		// Batch API specifically, so it only runs when the selected provider is
		// OpenAI itself; otherwise the live send path keeps using the static
		// library and admin generation returns "not configured".
		var generationClient *generation.GenerationClient
		if (aiProviderName == "" || aiProviderName == "openai") && aiKey != "" {
			generationClient = generation.NewClient(aiKey)
		}
		warmupContentService = warmupcontent.NewService(warmupContentRepo, generationClient)

		// Writing assistant generator: the OpenAI-compatible provider implements
		// WritingGenerator directly; the Anthropic connector delegates writing to
		// the dedicated Anthropic writing client. Neither configured => nil => 503.
		if wg, ok := aiProvider.(generation.WritingGenerator); ok {
			writingGenerator = wg
		} else if aiProviderName == "anthropic" && aiKey != "" {
			writingGenerator = generation.NewAnthropicClient(aiKey)
		}
		creditRepository = repository.NewCreditRepository(primaryDB)
		aiSettingsRepository = repository.NewAISettingsRepository(primaryDB)
		creditService = credits.NewService(creditRepository, aiSettingsRepository, cache)
		webhookRepository := repository.NewWebhookRepository(primaryDB.Pool)
		webhookService := webhook.NewService(webhookRepository)
		webhookServiceForHandler = webhookService

		integrationRepository := repository.NewIntegrationRepository(primaryDB.Pool)
		// OAuth 2.1 authorization server (third-party app registration + token flow).
		oauthService = oauth.NewService(repository.NewOAuthRepository(primaryDB.Pool), cache)
		// Enforce the per-app webhook-domain allowlist on app-scoped endpoints (at
		// write time, and re-checked at delivery time via the worker below).
		webhookService.WireAppDomainResolver(oauthService.AllowedWebhookDomains)
		// Materialize per-org webhook endpoints when an app is authorized/revoked or
		// its webhook config changes (the app-level subscription model).
		oauthService.WireWebhookSync(webhookRepository)
		// integrationServiceForHandler is constructed after cipherService below —
		// OAuth/secret sealing depends on the envelope-encryption service.
		contactRepoForHandler = contactRepostory

		// Drain the webhook delivery queue in-process. Multiple replicas are
		// safe because ClaimDueDeliveries uses SELECT … FOR UPDATE SKIP LOCKED.
		// Cache enables the per-endpoint delivery rate limit; AppDomains re-checks
		// app-scoped endpoint hosts at delivery time.
		webhookWorker := webhook.NewDeliveryWorker(webhookRepository, webhook.DeliveryWorkerOptions{
			Cache:      cache,
			AppDomains: oauthService.AllowedWebhookDomains,
		})
		go webhookWorker.Run(ctx)
		campaignProgressRepository := repository.NewCampaignProgressRepository(primaryDB.Pool)
		campaignLogRepository := repository.NewCampaignLogRepository(primaryDB)
		warmupService = warmupapp.NewService(warmupRepository)
		// Fan out warmup health transitions to customer webhooks.
		warmupService.WireWebhooks(webhookService, emailRepostory)

		tzService = tz.NewService()

		// Initialize new services for trial, feature gates, and worker assignment.
		// The trial service seeds the free plan's monthly AI-credit allowance at
		// trial start (planRepo + creditService, both already constructed above).
		trialService = trial.NewService(subscriptionRepository, userRepostory, planRepository, creditService)
		featureGateService = feature.NewService(subscriptionRepository, planRepository)
		workerAssignmentService = worker.NewAssignmentService(workerRepository, subscriptionRepository, planRepository)
		subscriptionService = subscription.NewService(subscriptionRepository, planRepository)
		// dailyThrottleService needs the cache that's constructed
		// earlier in main; instantiate up here so org create can use it.
		if dailyThrottleService == nil {
			dailyThrottleService = dailythrottle.NewService(cache)
		}
		organizationService = organization.NewService(organizationRepository, subscriptionRepository, userRepostory, dailyThrottleService)

		// Plan-based webhook/integration fan-out throttle. The cap scales with
		// the org's effective mailbox allowance (see WebhookDispatchLimit) so a
		// campaign "notify" action can't flood a customer's endpoints. Wired here
		// now that organizationService exists; the resolved cap is cached so this
		// resolver is not hit on every dispatched event.
		webhookServiceForHandler.WireThrottle(cache, organizationService.WebhookDispatchLimit)

		// Billing. Default (BILLING_PROVIDER=none, the self-host default): a no-op
		// Stripe service so the backend boots with no Stripe keys and every
		// feature is unlocked by the feature gate. BILLING_PROVIDER=stripe wires
		// the real Stripe integration (keys required).
		if config.BillingProvider() == "stripe" {
			stripeCfg, err := cfg.LoadStripeConfig(ctx)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
			stripeService = stripe.NewService(stripeCfg, subscriptionRepository, planRepository, workerAssignmentService, discountService)
		} else {
			stripeService = stripe.NewDisabledService()
		}

		tokenService = token.NewService(primaryDB, tokenRepostory, cache, geoloc, authCfg.AuthSecret)
		userService = user.NewService(userRepostory, cache)

		// Organization-wide audit trail (who did what, when, from where).
		auditRepository := repository.NewAuditRepository(primaryDB.Pool)
		auditService = audit.NewService(auditRepository, streamingPublisher)
		// Bridge audited mutations to typed customer webhooks (campaign/contact/
		// template/CRM/team/role/settings/subscription .created/.updated/.deleted).
		auditService.WireWebhookDispatcher(webhookService)

		// Wire AI-credit grants into the Stripe webhook flow: monthly allowance
		// reset on invoice.paid and top-up fulfillment on checkout.session.completed
		// (mode=payment). The audit logger fires AUDIT_CREATED so teammates' credit
		// views refresh live via the spine.
		stripeService.WireCredits(creditService, auditService)

		// Credit watch: after every fresh debit it fires the low-balance alert
		// (once per day) and, when enabled, buys the configured pack off-session
		// (auto top-up). Runs detached so charges are never slowed.
		creditWatch := creditwatch.New(aiSettingsRepository, creditRepository, cache, streamingPublisher, stripeService)
		creditService.SetMonitor(creditWatch.OnBalanceChanged)

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
		// TOTP 2FA: the secret is sealed with a server-wide key (the per-user DEK
		// is unreachable at login time). Wire the challenger into auth so the login
		// gate can issue a pending challenge.
		twofaService = twofa.NewService(repository.NewTOTPRepository(primaryDB.Pool), userRepostory, tokenService, cache, twofa.DeriveKey(authCfg.TwoFASecret))
		authService.WireTwoFA(twofaService)

		// Deployment-level auth policy: whether an emailed code gates a login,
		// whether signups are open, whether verification is required. Resolved
		// against the mail transport, because a code nobody can receive cannot
		// be a login requirement.
		authPolicy = config.LoadAuthPolicy(mailTransport != nil && mailTransport.Delivers)
		authService.WireDeployment(authPolicy, mailTransport != nil && mailTransport.Delivers)
		// Invitations answer to the same policy as the signup form.
		if organizationService != nil {
			organizationService.WireAuthPolicy(authPolicy)
		}
		// The database-backed settings: invitation expiry, the invite-link
		// switch, and whether an invitee may create their own account.
		if instanceSettings != nil {
			authService.WireInstanceSettings(instanceSettings)
			if organizationService != nil {
				organizationService.WireInstanceSettings(instanceSettings)
			}
		}
		log.Printf("Auth policy: login_code=%s registration=%s (DISABLE_REGISTRATION) email_verification=%t sso_auto_provision=%t",
			authPolicy.LoginCode, authPolicy.Registration, authPolicy.RequireEmailVerification, authPolicy.SSOAutoProvision)

		// Native-app social sign-in. Declared as the interface type so an
		// unconfigured provider stays a true nil (no typed-nil pitfall) and
		// the service reports it as unavailable.
		var appleIDTokens, googleIDTokens auth.IDTokenVerifier
		if authCfg.AppleIOSBundleID != "" {
			appleIDTokens = idtoken.AppleVerifier(authCfg.AppleIOSBundleID)
		}
		if authCfg.GoogleIOSClientID != "" {
			googleIDTokens = idtoken.GoogleVerifier(authCfg.GoogleIOSClientID)
		}
		authService.WireExternalIDTokens(appleIDTokens, googleIDTokens)

		// Federated identities keyed on (issuer, subject). Without this,
		// external sign-in resolves accounts by email alone, which is only safe
		// for issuers that control their own email namespace.
		authService.WireIdentities(repository.NewIdentityRepository(primaryDB.Pool))

		// Generic OIDC. Discovery runs at boot: an unreachable issuer is a
		// configuration error worth surfacing now rather than as a login button
		// that always fails.
		if issuer := os.Getenv("OIDC_ISSUER_URL"); issuer != "" {
			oidcSvc, oerr := oidcauth.New(ctx, oidcauth.Config{
				IssuerURL:      issuer,
				ClientID:       os.Getenv("OIDC_CLIENT_ID"),
				ClientSecret:   os.Getenv("OIDC_CLIENT_SECRET"),
				RedirectURL:    oidcRedirectURL(),
				Scopes:         splitList(os.Getenv("OIDC_SCOPES")),
				AllowedDomains: splitList(os.Getenv("OIDC_ALLOWED_DOMAINS")),
				DefaultOrgID:   os.Getenv("OIDC_DEFAULT_ORG"),
				ProviderName:   os.Getenv("OIDC_PROVIDER_NAME"),
			})
			if oerr != nil {
				oidcDiscoveryErr = oerr.Error()
				log.Printf("Warning: OIDC disabled: %v", oerr)
			} else {
				oidcLogin = oidcSvc
				authService.WireOIDC(oidcSvc)
				log.Printf("OIDC login enabled (issuer %s)", oidcSvc.Issuer())
			}
		}
		externalAuthProviders = models.ExternalAuthProviders{
			AppleBundleID:     authCfg.AppleIOSBundleID,
			GoogleIOSClientID: authCfg.GoogleIOSClientID,
		}

		// Referral program. Wired bidirectionally with Stripe (webhooks reward and
		// claw back; the earnings ledger nets onto the referrer's customer balance)
		// and into auth so a signup carrying ?ref= is attributed to its referrer.
		referralRepository := repository.NewReferralRepository(primaryDB.Pool)
		referralService = referral.NewService(
			referralRepository,
			discountCodeRepository,
			planRepository,
			subscriptionRepository,
			userRepostory,
			auditService,
			os.Getenv("APP_URL"),
		)
		stripeService.WireReferral(referralService)
		referralService.WireBalancer(stripeService)
		authService.WireReferral(referralService)
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
		passkeysUsable = passkeysUsableFor(os.Getenv("APP_URL"))
		googleWebSignIn = authCfg.GoogleClientID != ""
		appleWebSignIn = authCfg.AppleAppID != ""
		authCache = cache
		warnDeploymentURLs(ctx, os.Getenv("APP_URL"))

		// First-owner bootstrap. A no-op once any user exists, so it runs on
		// every boot and only ever does something on a fresh install.
		bootstrapService = bootstrap.NewService(
			userRepostory,
			userService,
			organizationService,
			trialService,
			repository.NewAdminRepository(primaryDB.Pool),
			cache,
		)
		if berr := bootstrapService.Run(ctx); berr != nil {
			sentry.CaptureException(berr)
			log.Printf("Warning: bootstrap failed: %v", berr)
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
				// A remote worker reaches the internal API over the network, so
				// fall back to the public API URL. ENCRYPTED_KEYS_BACKEND_URL is
				// typically only set on workers themselves (compose points it at
				// the in-network hostname), leaving it empty here and shipping a
				// config the worker cannot use.
				EncryptedKeysBackendURL:  getenvDefault("ENCRYPTED_KEYS_BACKEND_URL", os.Getenv("API_PUBLIC_URL")),
				EncryptedKeysWorkerToken: os.Getenv("INTERNAL_API_TOKEN"),
				KMSProvider:              getenvDefault("KMS_PROVIDER", "local"),
				KMSLocalMasterKey:        os.Getenv("KMS_LOCAL_MASTER_KEY"),
				KMSAWSKeyID:              os.Getenv("KMS_AWS_KEY_ID"),
				CredentialsEncryptionKey: os.Getenv("CREDENTIALS_ENCRYPTION_KEY"),
				BlobProvider:             getenvDefault("BLOB_PROVIDER", "filesystem"),
				BlobBucket:               os.Getenv("BLOB_BUCKET"),
				BlobFSRoot:               os.Getenv("BLOB_FS_ROOT"),
				EventBusProvider:         os.Getenv("EVENTBUS_PROVIDER"),
				NATSURL:                  os.Getenv("NATS_URL"),
				CodecProvider:            os.Getenv("CODEC_PROVIDER"),
				BoxGoogleClientID:        os.Getenv("BOX_GOOGLE_CLIENT_ID"),
				BoxGoogleClientSecret:    os.Getenv("BOX_GOOGLE_CLIENT_SECRET"),
				BoxOutlookClientID:       os.Getenv("BOX_OUTLOOK_CLIENT_ID"),
				BoxOutlookClientSecret:   os.Getenv("BOX_OUTLOOK_CLIENT_SECRET"),
			},
			getenvDefault("WORKER_INSTALLER_PATH", "/app/scripts/install-worker.sh"),
		)

		// Releases service. Off by default for self-host (no vendor image
		// auto-roll, no GitHub polling on boot); set RELEASES_ENABLED=true to
		// point it at your own repo/registry.
		releasesService = releases.New(
			releases.Config{
				Enabled:         getenvDefault("RELEASES_ENABLED", "false") == "true",
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

		// apiCfg.Hostname is the bind address, not a reachable base. Building
		// the mailbox-connect redirect_uri from it sends the provider
		// "0.0.0.0:8080/addresses/google/callback".
		oauth2Cfg := config.LoadOauth2(oauthPublicBaseURL(apiCfg.Hostname))
		emailService = email.NewServiceWithWorker(
			emailRepostory,
			cipherService,
			featureGateService,
			warmupService,
			eventsPublisher,
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
		// Seed Graph delta cursors when the reconciler reloads mailboxes.
		emailService.WireGraphDelta(repository.NewEmailGraphDeltaRepository(primaryDB))
		// The Gmail equivalent: without it a reloaded mailbox re-bootstraps its
		// history cursor and skips everything that arrived since the last sync.
		emailService.WireEmailHistoryID(repository.NewEmailHistoryIDRepository(primaryDB))
		// Sync fair use: the policy a mailbox syncs under, its resumable
		// backfill state, and the saved IMAP folder cursors.
		emailSyncStateRepository = repository.NewEmailSyncStateRepository(primaryDB)
		emailService.WireSyncState(emailSyncStateRepository)
		emailService.WireMailboxes(repository.NewMailboxRepository(primaryDB))
		if instanceSettings != nil {
			emailService.WireSyncBudget(instanceSettings)
		}
		analyticsRepository := repository.NewAnalyticsRepository(primaryDB)
		emailAccountErrorRepository := repository.NewEmailAccountErrorRepository(primaryDB)
		analyticsService = analytics.NewService(analyticsRepository, emailRepostory, campaignRepostory, emailAccountErrorRepository, warmupRepository)

		rateLimitRepository := repository.NewRateLimitRepository(primaryDB)
		rateLimitService = ratelimit.NewService(cache, rateLimitRepository)
		sequenceService = sequence.NewService(sequenceRepostory)
		contactService = contact.NewService(contactRepostory, subscriptionRepository, planRepository, streamingPublisher)

		// On-demand Google Sheets -> leads sync (backend-only / control plane).
		// Reuses the integration service for the Google token + sheet reads and
		// the contact service for the upsert. No worker / scheduler involvement.
		leadSyncRepository := repository.NewLeadSyncRepository(primaryDB.Pool)
		leadSyncServiceForHandler = leadsync.NewService(leadSyncRepository, integrationServiceForHandler, contactService)

		apiKeyService = apikey.NewService(cache, apiKeyRepository)
		crmService = crm.NewService(crmRepository)
		teamRepository := repository.NewTeamRepository(primaryDB.Pool)
		teamService = team.NewService(teamRepository)
		socketService = socket.NewService(cache, tokenService)

		// Task scheduler. Default (TASKS_PROVIDER=local): an in-process Postgres
		// poller, started below once the dispatch handler exists — no GCP, no
		// webhook, no emulator. TASKS_PROVIDER=gcloud keeps Google Cloud Tasks.
		var tasksClient tasksched.Scheduler
		var localTasks *tasksched.Local
		if config.TasksProvider() == "gcloud" {
			cloudTasksCfg, err := cfg.LoadCloudTasksConfig(ctx)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
			gclient, err := gtasks.NewClient(ctx, cloudTasksCfg.QueueName, cloudTasksCfg.WebhookURL, serviceAccount, cloudTasksCfg.EmulatorHost)
			if err != nil {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
			tasksClient = gclient
		} else {
			localTasks = tasksched.NewLocal(taskRepository, localTasksPollInterval(), 0)
			tasksClient = localTasks
		}

		// Template & email send services
		templateService = template.NewService(templateRepository)

		// Sending behaviour. Wired onto the scheduler rather than passed to its
		// constructor so a deployment without it keeps every mailbox on the
		// legacy fixed cap and min-gap path.
		behaviorRepository := repository.NewBehaviorRepository(primaryDB.Pool)
		behaviorService = behaviorapp.NewService(behaviorRepository, emailRepostory)

		schedulerService := scheduler.NewSchedulerService(taskRepository, warmupRepository, campaignProgressRepository, emailRepostory, campaignRepostory, contactRepostory, campaignLogRepository)
		if aware, ok := schedulerService.(scheduler.BehaviorAware); ok {
			aware.WireBehavior(behaviorService)
		}
		campaignService = campaign.NewService(campaignRepostory, taskRepository, emailRepostory, campaignLogRepository, featureGateService, dailyThrottleService, schedulerService, tasksClient, streamingPublisher)
		emailSendService = emailsend.NewService(taskRepository, emailRepostory, userRepostory, schedulerService, tasksClient, featureGateService, dailyThrottleService)
		composeService = compose.NewService(emailRepostory, repository.NewComposeRepository(primaryDB))
		// uniboxService is constructed here (rather than alongside the
		// other service constructors above) because cancel-scheduled
		// needs the Cloud Tasks client for best-effort DeleteTask, and
		// tasksClient isn't initialised until the Cloud Tasks config
		// block runs.
		uniboxService = unibox.NewService(cache, s3, uniboxRepository, taskRepository, tasksClient)

		// Org AI skills (playbooks): CRUD for settings + prompt injection + the
		// load_skill tool source.
		skillsService = skills.NewService(repository.NewSkillRepository(primaryDB))

		// The advanced-outreach brain also answers "is this recipient
		// suppressed?", which the compose/reply send tools consult before
		// sending. Constructed here (ahead of its event wiring below) so the AI
		// tool registry can hold the suppression checker.
		advancedService = advanced.NewService(
			advancedRepository,
			campaignRepostory,
			emailRepostory,
			taskRepository,
			contactRepostory,
			campaignProgressRepository,
			crmRepository,
			categoryRepostory,
			uniboxRepository,
			tasksClient,
			warmupService,
		)

		// Shared AI tool registry: every tool calls a service-layer function as
		// the invoking user, so the dashboard agent (M3) and MCP server (M8) can
		// never exceed the caller's permissions. Built once here with the same
		// service instances the HTTP handlers use.
		aiToolRegistry = aitools.BuildRegistry(aitools.Deps{
			Contacts:     contactService,
			CRM:          crmService,
			Campaigns:    campaignService,
			Analytics:    analyticsService,
			Unibox:       uniboxService,
			Automations:  integrationServiceForHandler,
			Audit:        auditService,
			Search:       aiSearch,
			Cache:        cache,
			Emails:       emailService,
			EmailSend:    emailSendService,
			Compose:      composeService,
			Warmup:       warmupService,
			Sequences:    sequenceService,
			Org:          organizationService,
			APIKeys:      apiKeyService,
			Webhooks:     webhookServiceForHandler,
			Subscription: subscriptionService,
			Advanced:     advancedService,
			FeatureGate:  featureGateService,
			Skills:       skillsService,
			AppBaseURL:   cfg.GetStringOptional(ctx, "APP_BASE_URL", "app_base_url", ""),
		})

		// Connected MCP servers (client direction): their enabled tools are
		// contributed to the dashboard agent per-org (approval-gated, never
		// auto-allowed).
		mcpService = mcp.NewService(repository.NewMCPRepository(primaryDB), cipherService)
		aiToolRegistry.AddDynamicSource(mcpService)

		// Dashboard AI agent: sessions + streamed, approval-gated, credit-charged
		// runs over the tool registry. Only constructed when a provider is set.
		if aiProvider != nil {
			aiAgentService = aiagent.NewService(
				repository.NewAgentRepository(primaryDB),
				aiToolRegistry, aiProvider, creditService, featureGateService, auditService, skillsService,
				aiagent.NewVoicePreamble(organizationService),
				organizationService,
			)
			// Contact research agent + its bounded background drain pool.
			researchService = research.NewService(
				repository.NewResearchRepository(primaryDB),
				aiToolRegistry, aiProvider, creditService, featureGateService,
				contactService, organizationService, streamingPublisher, skillsService,
			)
			researchService.StartDrainPool(ctx)
		}
		// Fan reply + bounce events from the advanced-outreach brain out to
		// customer webhooks AND third-party integration actions (Slack / CRM).
		advancedService.WireDispatcher(webhookService)
		// Let instant action chains (reply/open/click branches) launch a
		// "run_automation" node, the same flow the scheduler runs at a step
		// boundary. Backend ingests deliverability + can process replies too.
		advancedService.WireAutomationRunner(integrationServiceForHandler)
		// Wire native (Warmbly-internal) automation actions + realtime now that
		// the advanced/contact/org services exist (the integration service was
		// constructed earlier).
		integrationServiceForHandler.SetNativeActions(nativeactions.Adapter{
			Adv:      advancedService,
			Contacts: contactRepostory,
			Orgs:     organizationRepository,
		})
		integrationServiceForHandler.SetPublisher(streamingPublisher)
		// AI automation nodes (ai_step / ai_switch) run over the same provider +
		// credit ledger as the rest of the AI layer. Nil provider (no AI_PROVIDER)
		// leaves the nodes returning a clean "not available".
		integrationServiceForHandler.SetAI(aiProvider, creditService)
		// The AI switch's optional web search shares the same pluggable backend as
		// the campaign switch and dashboard agent.
		integrationServiceForHandler.SetAISearch(aiSearch)
		// Port reply-classifier Layer 3 onto the platform provider (OpenAI-first,
		// self-hostable). Platform-paid, never charged to org credits. Nil provider
		// leaves Layer 3 disabled (the ambiguous middle resolves to "unknown").
		if aiProvider != nil {
			replyclassify.SetModelClassifier(func(ctx context.Context, system, user string) (string, error) {
				res, err := aiProvider.Complete(ctx, generation.CompletionRequest{System: system, Prompt: user, MaxTokens: 16, Temperature: generation.Deterministic()})
				if err != nil {
					return "", err
				}
				return res.Text, nil
			})
		}
		// In-app notifications: API reads/writes happen here; also wire the gate
		// onto the backend's advanced service (deliverability webhooks can ingest
		// here too).
		notificationService = notification.NewService(repository.NewNotificationRepository(primaryDB.Pool), streamingPublisher)
		notificationService.WireDelivery(emailNotificationService, integrationServiceForHandler, userRepostory, organizationRepoForHandler)
		// Mobile push (APNs): device registration always works; delivery only
		// activates when the APNS_* env is configured. The Redis client backs
		// the shared immediate-then-digest push window. The sender stays a nil
		// interface (not a typed-nil *apns.Client) when unconfigured.
		var pushSender notification.PushSender
		if apnsClient, aerr := apns.FromEnv(); aerr != nil {
			log.Printf("Warning: APNs push disabled: %v", aerr)
		} else if apnsClient != nil {
			pushSender = apnsClient
		}
		notificationService.WirePush(pushSender, repository.NewDeviceTokenRepository(primaryDB.Pool), cache.Client)
		advancedService.WireNotifier(notificationService)
		// New-device sign-in alerts: the token service fires this on session
		// creation from an unrecognized device, delivered as a security
		// notification (in-app + email per the user's channels).
		tokenService.WireSignInAlerter(notification.NewSignInAlerter(notificationService))
		advancedService.WireRealtime(streamingPublisher)
		// Inbox agent (M10): the draft repo the review endpoints read, plus the
		// agent wired onto the advanced service so any reply processed here also
		// drafts. Paid + opt-in checked inside; nil provider leaves it inert.
		aiDraftRepo = repository.NewAIDraftRepository(primaryDB.Pool)
		advancedService.WireInboxAgent(inboxagent.NewService(
			aiProvider, creditService, featureGateService,
			organizationRepository, uniboxRepository, skillsService,
			contactRepostory, aiDraftRepo, streamingPublisher,
		))
		emailSender := tasks.NewEmailSender(emailRepostory, eventsPublisher)
		tasksService = tasks.NewService(
			tasksClient,
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
			attachmentRepoForHandler,
			trackedLinkRepository,
			integrationServiceForHandler, // AutomationRunner for campaign run_automation steps
		)
		// Campaign "ai" sequence steps run over the same provider + credit
		// ledger as the automation AI nodes. Nil provider leaves them
		// returning a clean "not available".
		tasksService.SetAI(aiProvider, creditService)
		tasksService.SetAISearch(aiSearch)
		// Research-mode AI variables run a bounded web-research agent over the
		// shared tool registry at send time.
		tasksService.SetAITools(aiToolRegistry)

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

		// Start trial expiration job in background. Only meaningful with Stripe
		// billing: self-host unlocks every feature regardless of trial state, so
		// there is nothing to expire and no upgrade to nudge toward.
		if config.BillingProvider() == "stripe" {
			trialExpirationJob := jobs.NewTrialExpirationJobWithDB(subscriptionRepository, primaryDB.Pool, emailNotificationService)
			trialExpirationJob.WireNotifier(notificationService)
			trialScheduler := jobs.NewTrialExpirationScheduler(trialExpirationJob, 1*time.Hour)
			go trialScheduler.Start(ctx)
		}

		// Local task dispatcher (TASKS_PROVIDER=local): the in-process poller
		// that fires due tasks by id. Started here, once tasksService exists to
		// handle them. Under gcloud this stays nil (the webhook drives dispatch).
		if localTasks != nil {
			go localTasks.Run(ctx, func(taskID string) {
				if xerr := tasksService.HandleTask(&proto.ProcessTask{TaskId: taskID}); xerr != nil {
					log.Printf("local task dispatch failed for %s: %v", taskID, xerr)
				}
			})
		}

		// Warmup reconciler: seed/repair warmup chains for mailboxes that are
		// warming or backing a live campaign (the health-check lane). This is
		// the bootstrap — enabling warmup or starting a campaign doesn't itself
		// enqueue the first warmup task.
		go tasksService.StartWarmupReconciler(ctx, 10*time.Minute)

		// Campaign reconciler: re-seed active campaigns whose self-perpetuating
		// task chain died (a swallowed enqueue, a worker bounce mid-tick, or a
		// crash between send and enqueue). Campaigns have no other bootstrap once
		// started, so without this a stranded campaign stops sending forever.
		go tasksService.StartCampaignReconciler(ctx, 5*time.Minute)

		// Worker reconciler: assign + (re)load every active mailbox onto its
		// worker. Workers hold accounts in memory only, so this is what makes a
		// freshly onboarded account send/sync and re-seeds a restarted worker.
		go emailService.StartWorkerReconciler(ctx, 60*time.Second)

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

		// Workspace archives: export a whole organization to a portable file
		// and import one back, so a workspace can move between instances.
		// It needs both key domains — the instance credential key for mailbox
		// credentials and the org DEK for everything else — because neither is
		// portable and both have to be re-keyed on the way through.
		orgTransferService = orgtransfer.NewService(
			repository.NewOrgTransferRepository(primaryDB),
			cipherService,
			credEncrypter,
			s3,
			orgtransfer.InstanceInfo{
				PublicURL:  os.Getenv("APP_URL"),
				AppVersion: os.Getenv("APP_VERSION"),
			},
		)
		orgTransferJob := jobs.NewOrgTransferJob(orgTransferService)
		orgTransferScheduler := jobs.NewOrgTransferScheduler(orgTransferJob, 1*time.Hour)
		go orgTransferScheduler.Start(ctx)

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

		// Auto-pause guardrails: pause any active campaign whose bounce,
		// complaint, or reply rate has left the band its owner set. Fifteen
		// minutes is fast enough that a campaign cannot do much damage inside
		// one window at the platform's per-mailbox defaults. The same tick
		// prunes expired daily sending plans.
		guardrailService := guardrail.NewService(
			repository.NewGuardrailRepository(primaryDB.Pool),
			campaignLogRepository,
			auditService,
			notificationService,
		)
		guardrailJob := jobs.NewGuardrailJob(guardrailService, behaviorRepository)
		guardrailScheduler := jobs.NewGuardrailScheduler(guardrailJob, 15*time.Minute)
		go guardrailScheduler.Start(ctx)

		// Advisor. Detection is deterministic Go over a per-org snapshot and
		// always runs; the narrator is optional and only rewrites the card copy,
		// so an install with no LLM provider still gets every recommendation.
		// Fixes execute through the AI tool registry as the invoking member, so
		// the Advisor can never apply a change that member could not make by
		// hand. Registering its read tools here (rather than in BuildRegistry)
		// closes the loop between the two without an import cycle.
		advisorRepository = repository.NewAdvisorRepository(primaryDB)
		var advisorNarrator *advisor.Narrator
		if aiProvider != nil {
			advisorNarrator = advisor.NewNarrator(
				aiProvider,
				aiagent.NewVoicePreamble(organizationService),
				advisorTier{featureGateService},
				advisorRepository,
			)
		}
		// The agent fix reuses the assistant's provider, tool registry, and
		// credit meter, so a finding a settings change cannot resolve (broken
		// copy, a list full of shared inboxes) still has a way to be fixed.
		var advisorAgent *advisor.AgentDeps
		if aiProvider != nil {
			advisorAgent = &advisor.AgentDeps{
				Agent:   aiProvider,
				Tools:   aiToolRegistry,
				Credits: creditService,
				Tier:    advisorTier{featureGateService},
			}
		}
		advisorService = advisor.NewService(advisorRepository, aiToolRegistry, advisorNarrator, auditService,
			advisorMembers{organizationService}, advisorAgent,
			advisor.WithTrackingHost(emailCfg.TrackingDomain))
		aitools.RegisterAdvisorTools(aiToolRegistry, advisorService)
		go (&advisor.Runner{Repo: advisorRepository, Service: advisorService}).Run(ctx)

		addr = apiCfg.Hostname
		ginMode = apiCfg.GinMode
		websocketURI = apiCfg.WebsocketURI
		allowedOrigins = apiCfg.AllowedOrigins

		// Infrastructure liveness probes for the admin System Status page.
		// Wired here because the concrete clients live in this scope.
		systemChecker = sysstatus.New()
		systemChecker.Add("postgres", func(ctx context.Context) error { return primaryDB.Ping(ctx) })
		systemChecker.Add("redis", func(ctx context.Context) error { return cache.Ping(ctx).Err() })
		switch bus.Name() {
		case "kafka":
			systemChecker.Add("kafka", sysstatus.TCPCheck(kafkaBootstrapServers))
		case "nats":
			systemChecker.Add("nats", sysstatus.TCPCheck(strings.TrimPrefix(getenvDefault("NATS_URL", "nats://localhost:4222"), "nats://")))
		}
		if sr := os.Getenv("SCHEMA_REGISTRY_URL"); sr != "" {
			systemChecker.Add("schema-registry", sysstatus.HTTPCheck(strings.TrimRight(sr, "/")+"/subjects"))
		}
		if hu := wsHealthURL(websocketURI); hu != "" {
			systemChecker.Add("realtime", sysstatus.HTTPCheck(hu))
		}
		if v := os.Getenv("TRACKING_SERVICE_URL"); v != "" {
			systemChecker.Add("tracking", sysstatus.HTTPCheck(strings.TrimRight(v, "/")+"/health"))
		}
	}

	// The facts only boot knows, handed to the configuration and health pages so
	// they report what this process actually resolved rather than re-reading the
	// environment and drifting from it.
	instanceRuntime := &instanceconfig.Runtime{
		CORSOrigins:       splitList(os.Getenv("CORS_ALLOW_ORIGINS")),
		WebAuthnRPID:      os.Getenv("WEBAUTHN_RP_ID"),
		WebAuthnRPOrigins: splitList(os.Getenv("WEBAUTHN_RP_ORIGINS")),
		OIDCRedirectURL:   oidcRedirectURL(),
		OIDCConfigured:    oidcLogin != nil,
		OIDCDiscoveryErr:  oidcDiscoveryErr,
		MailTransportKind: mailTransportKind(mailTransport),
		MailDelivers:      mailTransport != nil && mailTransport.Delivers,
		PasskeysUsable:    passkeysUsable,
		InsecureDefaults:  insecureDefaultsInUse,
		Policy:            authPolicy,
		WebsocketURL:      os.Getenv("WEBSOCKET_URL"),
	}
	instanceChecks := instancecheck.New(instancecheck.Deps{
		Runtime:   instanceRuntime,
		Transport: mailTransport,
		Policy:    authPolicy,
		DB:        instanceChecksDB,
		Cache:     authCache,
	})

	h := &handler.Handler{
		AuthService:           authService,
		ExternalAuthProviders: externalAuthProviders,

		GoogleWebSignIn:  googleWebSignIn,
		AppleWebSignIn:   appleWebSignIn,
		OIDCEnabled:      oidcLogin != nil,
		MailDelivers:     mailTransport != nil && mailTransport.Delivers,
		MailTransport:    mailTransportKind(mailTransport),
		MailTransportRef: mailTransport,
		BootstrapService: bootstrapService,
		PasskeysUsable:   passkeysUsable,

		InstanceRuntime:  instanceRuntime,
		InstanceChecks:   instanceChecks,
		InstanceSettings: instanceSettings,

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

		TzService:           tzService,
		SocketService:       socketService,
		TasksService:        tasksService,
		NotificationService: notificationService,
		TwoFAService:        twofaService,

		// API Keys
		APIKeyService: apiKeyService,

		// Subscription & billing
		SubscriptionService: subscriptionService,
		StripeService:       stripeService,
		DiscountService:     discountService,
		ReferralService:     referralService,

		// Trial & feature gates
		TrialService:            trialService,
		FeatureGateService:      featureGateService,
		WorkerAssignmentService: workerAssignmentService,

		// Organization & IAM
		OrganizationService: organizationService,

		// CRM
		CRMService: crmService,

		// Teams
		TeamService: teamService,

		// Email send & templates
		TemplateService:  templateService,
		EmailSendService: emailSendService,
		ComposeService:   composeService,

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

		// AI writing assistant + credit ledger
		CreditService:    creditService,
		WritingGenerator: writingGenerator,
		AIProvider:       aiProvider,
		AISearch:         aiSearch,
		AITools:          aiToolRegistry,
		AIAgentService:   aiAgentService,
		ResearchService:  researchService,
		SkillsService:    skillsService,
		MCPService:       mcpService,
		AIDraftRepo:      aiDraftRepo,

		// Pre-send email verification
		EmailVerifyService: emailVerifyService,

		// Seed inbox-placement testing
		PlacementRepo:    placementRepository,
		PlacementService: placementService,

		BehaviorService:   behaviorService,
		AdvisorService:    advisorService,
		AdvisorRepository: advisorRepository,

		// Third-party integrations
		IntegrationService: integrationServiceForHandler,
		ContactRepo:        contactRepoForHandler,
		StreamingPublisher: streamingPublisher,

		// OAuth 2.1 authorization server
		OAuthService: oauthService,

		// On-demand Google Sheets -> leads sync
		LeadSyncService: leadSyncServiceForHandler,

		WebsocketURI: websocketURI,

		// Object storage + direct repository handles for handlers
		// without a dedicated service layer (avatars, etc.).
		Storage:                  s3ForHandler,
		EncryptedKeys:            encryptedKeys,
		EmailMessageMap:          emailMessageMapForHandler,
		EmailSyncState:           emailSyncStateRepository,
		TrackedLinks:             trackedLinkRepository,
		UserRepo:                 userRepoForHandler,
		OrgRepo:                  organizationRepoForHandler,
		AttachmentRepo:           attachmentRepoForHandler,
		StorageBackendRepo:       storageBackendRepo,
		CloudCredentialRepo:      cloudCredentialRepo,
		ProvisioningTemplateRepo: provisioningTemplateRepo,
		ProvisioningJobRepo:      provisioningJobRepo,
		ProvisioningPolicyRepo:   provisioningPolicyRepo,

		// Danger zone
		DangerZoneService:  dangerZoneService,
		OrgTransferService: orgTransferService,

		// Admin System Status probes
		SystemChecker: systemChecker,

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
		OAuthService:        oauthService,
		Cache:               authCache,
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

// localTasksPollInterval is how often the in-process dispatcher scans for due
// tasks. Default 1s; override with TASKS_LOCAL_POLL_INTERVAL (a Go duration).
func localTasksPollInterval() time.Duration {
	d, err := time.ParseDuration(getenvDefault("TASKS_LOCAL_POLL_INTERVAL", "1s"))
	if err != nil || d <= 0 {
		return time.Second
	}
	return d
}

// wsHealthURL derives the realtime service's HTTP /health URL from the
// public websocket URI (ws[s]://host[:port]/... -> http[s]://host[:port]/health).
func wsHealthURL(wsURI string) string {
	if wsURI == "" {
		return ""
	}
	u, err := url.Parse(wsURI)
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	default:
		u.Scheme = "http"
	}
	u.Path = "/health"
	u.RawQuery = ""
	return u.String()
}

// advisorTier adapts the feature gate to the advisor narrator's model-tier
// lookup. A gate error is treated as "not paid": the only consequence is that
// a card's copy is rewritten by the cheaper model.
type advisorTier struct{ gate feature.FeatureGateService }

func (t advisorTier) IsPaid(ctx context.Context, orgID uuid.UUID) bool {
	if t.gate == nil {
		return false
	}
	paid, err := t.gate.IsPaidOrganization(ctx, orgID)
	return err == nil && paid
}

// advisorMembers resolves the autopilot actor's permissions. An error here
// means the member is gone or was never in this org, and autopilot fails closed
// on it rather than falling back to acting with no permission mask at all.
type advisorMembers struct {
	orgs organization.OrganizationService
}

func (m advisorMembers) MemberPermissions(ctx context.Context, orgID, userID uuid.UUID) (models.OrganizationPermission, error) {
	if m.orgs == nil {
		return 0, errors.New("organization service unavailable")
	}
	member, xerr := m.orgs.GetMembership(ctx, orgID, userID)
	if xerr != nil {
		return 0, xerr
	}
	if member == nil {
		return 0, errors.New("not a member of this organization")
	}
	return member.Permissions, nil
}
