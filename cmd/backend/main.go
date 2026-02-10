package main

import (
	"context"
	"log"
	"os"
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
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/crm"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/group"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/socket"
	"github.com/warmbly/warmbly/internal/app/stripe"
	"github.com/warmbly/warmbly/internal/app/subscription"
	"github.com/warmbly/warmbly/internal/app/template"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/cdb"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/jobs"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/pkg/captcha"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
)

func main() {
	var addr string
	var ginMode string

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

	// Notifications
	var emailNotificationService notify.EmailNotificationService

	// Pub/Sub for realtime streaming
	var streamingPublisher *pubsub.StreamingPublisher

	{
		ctx := context.Background()

		// Load config with env-first approach
		cfg, err := config.NewConfig(ctx)
		if err != nil {
			log.Fatal(err)
		}

		if cfg.Env == "prod" {
			sentryDsn, err := cfg.LoadSentryDSNBackend(ctx)
			if err != nil {
				log.Fatal(err)
			}

			err = sentry.Init(sentry.ClientOptions{
				Dsn:            sentryDsn,
				SendDefaultPII: true,
			})
			if err != nil {
				log.Fatal(err)
			}
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
		if cfg.Env == "prod" {
			masterKey += "-dev"
		}

		kms, err := kms.New(ctx, awscfg, masterKey)
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

		astraConfig, err := cfg.LoadAstraConfig(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		cassandraDB, err := cdb.NewClient(astraConfig)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

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

		emailNotificationService, err = notify.NewEmailNotficiationService(
			ctx,
			emailCfg.EmailName,
			emailCfg.EmailAddress,
		)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
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

		captcha := captcha.NewTurnstile(authCfg.TurnstileSecret)

		userRepostory := repository.NewUserRepostory(primaryDB, kms)
		authRepostory := repository.NewAuthRepostory(primaryDB)
		tokenRepostory := repository.NewTokenRepostory(primaryDB)
		emailRepostory := repository.NewEmailRepostory(primaryDB)
		campaignRepostory := repository.NewCampaignRepostory(primaryDB)
		sequenceRepostory := repository.NewSequenceRepostory(primaryDB)
		contactRepostory := repository.NewContactRepostory(primaryDB)
		uniboxRepository := repository.NewUniboxRepository(cassandraDB)
		userEncryptedKeysRepository := repository.NewUserEncryptedKeysRepository(kms, dynamoDB)

		folderRepostory := repository.NewGroupRepostory(primaryDB, models.Folders)
		tagRepostory := repository.NewGroupRepostory(primaryDB, models.Tags)
		categoryRepostory := repository.NewGroupRepostory(primaryDB, models.Categories)

		// New repositories for subscription & worker management
		subscriptionRepository := repository.NewSubscriptionRepository(primaryDB.Pool)
		planRepository := repository.NewPlanRepository(primaryDB.Pool)
		workerRepository := repository.NewWorkerRepository(primaryDB.Pool)
		organizationRepository := repository.NewOrganizationRepository(primaryDB.Pool)
		taskRepository := repository.NewTaskRepository(primaryDB.Pool)
		apiKeyRepository := repository.NewAPIKeyRepository(primaryDB)
		crmRepository := repository.NewCRMRepository(primaryDB.Pool)
		templateRepository := repository.NewTemplateRepository(primaryDB.Pool)
		warmupRepository := repository.NewWarmupRepository(primaryDB.Pool)
		campaignProgressRepository := repository.NewCampaignProgressRepository(primaryDB.Pool)
		campaignLogRepository := repository.NewCampaignLogRepository(primaryDB)

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
		)
		userService = user.NewService(userRepostory, cache)
		cipherService = cipher.NewService(kms, cache, userEncryptedKeysRepository)
		eventsPublisher := events.NewPublisher(kafkaProducer, s3, avrov2Client, cipherService)
		emailService = email.NewServiceWithKafka(emailRepostory, cipherService, eventsPublisher, kafkaProducer, cache)
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

		addr = apiCfg.Hostname
		ginMode = apiCfg.GinMode
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

		// Notifications
		EmailNotificationService: emailNotificationService,
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

	api.Run(h, m, oidcH, addr, ginMode)
}
