package main

import (
	"context"
	"log"

	"github.com/MicahParks/keyfunc/v3"
	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/getsentry/sentry-go"
	"github.com/joho/godotenv"
	"github.com/meszmate/apple-go"
	"github.com/meszmate/google-go"
	"github.com/warmbly/warmbly/internal/api"
	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/group"
	"github.com/warmbly/warmbly/internal/app/role"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/socket"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/cdb"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/dynamo"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/infrastructure/secrets"
	"github.com/warmbly/warmbly/internal/infrastructure/ssm"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/pkg/captcha"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/repository"
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
	var roleService role.RoleService
	var socketService socket.SocketService
	var uniboxService unibox.UniboxService
	var cipherService cipher.CipherService

	var folderService group.GroupService
	var tagService group.GroupService
	var categoryService group.GroupService

	{
		godotenv.Overload("cmd/backend/.env")

		awscfg, err := awsconf.LoadDefaultConfig(context.Background())
		if err != nil {
			log.Fatal(err)
		}

		secrets, err := secrets.NewSecretsManagerClient(context.Background(), awscfg)
		if err != nil {
			log.Fatal(err)
		}

		params, err := ssm.NewSSMParameterStore(context.Background(), awscfg)
		if err != nil {
			log.Fatal(err)
		}

		cfg := config.Load(params, secrets)

		if cfg.Env == "prod" {
			sentryDsn, err := cfg.LoadSentryDSNBackend(context.Background())
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

		serviceAccount, err = cfg.LoadGoogleServiceAccount(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		keySet, err = keyfunc.NewDefaultCtx(context.Background(), []string{"https://www.googleapis.com/oauth2/v3/certs"})
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		apiCfg, err := cfg.LoadApiConfig(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		var masterKey string = "alias/master-key"
		if cfg.Env == "prod" {
			masterKey += "-dev"
		}

		kms, err := kms.New(context.Background(), awscfg, masterKey)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		geoPath, err := cfg.LoadGeoDBPath(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		geoloc, err := geo.New(geoPath)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		s3, err := storage.NewClient(context.Background(), awscfg, "main")
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		primaryDBEndpoint, err := cfg.LoadPrimaryDBEndpoint(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		primaryDB, err := db.New(context.Background(), primaryDBEndpoint)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		astraConfig, err := cfg.LoadAstraConfig(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		cassandraDB, err := cdb.NewClient(astraConfig)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		dynamoDB, err := dynamo.NewClient(context.Background(), awscfg)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		primaryRedis, err := cfg.LoadPrimaryRedisEndpoint(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		cache, err := cache.New(primaryRedis)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		emailCfg, err := cfg.LoadEmailConfig(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		emailNotificationService, err := notify.NewEmailNotficiationService(
			context.Background(),
			emailCfg.EmailName,
			emailCfg.EmailAddress,
		)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		authCfg, err := cfg.LoadAuthConfig(context.Background())
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

		appleAuth, err := apple.NewB64(
			authCfg.AppleAppID,
			authCfg.AppleTeamID,
			authCfg.AppleKeyID,
			authCfg.AppleKeySecret,
		)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		kafkaBootstrapServers, err := cfg.LoadKafkaBootstrapServers(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		kafkaSaslConfig, err := cfg.LoadKafkaConfigSasl(context.Background())
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		kafkaProducerConfig := kafka.NewProducer(kafkaBootstrapServers)
		kafkaProducerConfig.WithSASL(kafkaSaslConfig)

		kafkaProducer, err := kafkaProducerConfig.Connect()
		if err != nil {
			sentry.CaptureException(err)
			log.Fatal(err)
		}

		captcha := captcha.NewTurnstile(authCfg.TurnstileSecret)

		userRepostory := repository.NewUserRepostory(primaryDB, kms)
		authRepostory := repository.NewAuthRepostory(primaryDB)
		tokenRepostory := repository.NewTokenRepostory(primaryDB)
		emailRepostory := repository.NewEmailRepostory(primaryDB)
		campaignRepostory := repository.NewCampaignRepostory(primaryDB)
		sequenceRepostory := repository.NewSequenceRepostory(primaryDB)
		contactRepostory := repository.NewContactRepostory(primaryDB)
		roleRepostory := repository.NewRoleRepostory(primaryDB)
		uniboxRepository := repository.NewUniboxRepository(cassandraDB)
		userEncryptedKeysRepository := repository.NewUserEncryptedKeysRepository(kms, dynamoDB)

		folderRepostory := repository.NewGroupRepostory(primaryDB, models.Folders)
		tagRepostory := repository.NewGroupRepostory(primaryDB, models.Tags)
		categoryRepostory := repository.NewGroupRepostory(primaryDB, models.Categories)

		tzService = tz.NewService()

		tokenService = token.NewService(primaryDB, tokenRepostory, cache, geoloc, authCfg.AuthSecret)
		authService = auth.NewService(
			authRepostory,
			cache,
			captcha,
			tokenService,
			emailNotificationService,
			&models.ExternalAuth{
				GoogleAuth: googleAuth,
				AppleAuth:  appleAuth,
			},
		)
		userService = user.NewService(userRepostory, cache)
		cipherService = cipher.NewService(kms, cache, userEncryptedKeysRepository)
		emailService = email.NewService(emailRepostory, cipherService, kafkaProducer)
		campaignService = campaign.NewService(campaignRepostory)
		sequenceService = sequence.NewService(sequenceRepostory)
		contactService = contact.NewService(contactRepostory)
		roleService = role.NewService(cache, roleRepostory)
		socketService = socket.NewService(cache, tokenService)
		uniboxService = unibox.NewService(cache, s3, uniboxRepository)

		folderService = group.NewService(folderRepostory)
		tagService = group.NewService(tagRepostory)
		categoryService = group.NewService(categoryRepostory)

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
		RoleService:     roleService,
		UniboxService:   uniboxService,

		FolderService:   folderService,
		TagService:      tagService,
		CategoryService: categoryService,

		TzService:     tzService,
		SocketService: socketService,
	}

	m := &middleware.Handler{
		TokenService: tokenService,
	}

	oidcH := &middleware.OidcHandler{
		ServiceAccount: serviceAccount,
		KeySet:         keySet,
	}

	sentry.CaptureMessage("Starting the backend on " + addr)

	api.Run(h, m, oidcH, addr, ginMode)
}
