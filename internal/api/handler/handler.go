package handler

import (
	"github.com/warmbly/warmbly/internal/app/admin"
	"github.com/warmbly/warmbly/internal/app/adminoutreach"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/crm"
	"github.com/warmbly/warmbly/internal/app/dangerzone"
	"github.com/warmbly/warmbly/internal/app/discount"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	emailverifyapp "github.com/warmbly/warmbly/internal/app/emailverify"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/group"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/passkey"
	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/app/ratelimit"
	"github.com/warmbly/warmbly/internal/app/releases"
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
	"github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/app/warmupcontent"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/app/worker_orchestrator"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks"
)

type Handler struct {
	AuthService     auth.AuthService
	TokenService    token.TokenService
	PasskeyService  passkey.Service
	UserService     user.UserService
	EmailService    email.EmailService
	CampaignService campaign.CampaignService
	ContactService  contact.ContactService
	SequenceService sequence.SequenceService
	UniboxService   unibox.UniboxService

	FolderService   group.GroupService
	TagService      group.GroupService
	CategoryService group.GroupService

	TzService     tz.TzService
	SocketService socket.SocketService
	TasksService  tasks.TasksService

	// New services
	APIKeyService    apikey.APIKeyService
	AnalyticsService analytics.AnalyticsService
	AuditService     audit.AuditService
	RateLimitService ratelimit.RateLimitService

	// Subscription & billing
	SubscriptionService subscription.SubscriptionService
	StripeService       stripe.StripeService
	DiscountService     discount.DiscountService

	// Trial & feature gates
	TrialService            trial.TrialService
	FeatureGateService      feature.FeatureGateService
	WorkerAssignmentService worker.WorkerAssignmentService

	// Organization & IAM
	OrganizationService organization.OrganizationService

	// CRM
	CRMService crm.CRMService

	// Email send & templates
	TemplateService  template.TemplateService
	EmailSendService emailsend.EmailSendService

	// Admin
	AdminService         admin.AdminService
	AdminOutreachService adminoutreach.Service

	// Worker orchestration (SSH-driven lifecycle for admin-managed workers)
	WorkerOrchestrator *worker_orchestrator.Orchestrator
	WorkerRepo         repository.WorkerRepository
	CredentialsRepo    repository.CredentialsRepository
	ReleasesService    *releases.Service

	// Notifications
	EmailNotificationService notify.EmailNotificationService

	// Advanced outreach controls
	AdvancedService advanced.Service

	// Pre-send email verification (control-plane SMTP RCPT probe / pluggable
	// paid backend). Drops hard-bouncing addresses before a worker sends.
	EmailVerifyService emailverifyapp.Service

	// Warmup health
	WarmupService warmup.Service

	// Warmup routing rules — customer-defined preferences for premium-pool
	// partner selection (e.g. Gmail recipients from Google Workspace senders).
	WarmupRoutingRepo repository.WarmupRoutingRepository

	// Warmup content bank + offline AI generator (admin control/visibility).
	WarmupContentRepo    repository.WarmupContentRepository
	WarmupContentService warmupcontent.Service

	// Seed inbox-placement testing.
	PlacementRepo    repository.PlacementRepository
	PlacementService placement.Service

	// Customer-facing webhooks (subscribe → HMAC-signed delivery).
	WebhookService webhook.Service

	// Third-party integrations (Calendly, Cal.com, DMARC, Postmaster,
	// SNDS, Cloudflare, GoDaddy, Namecheap, Google Sheets).
	IntegrationService integration.Service
	ContactRepo        repository.ContactRepository

	// Public websocket URL used by frontend clients
	WebsocketURI string

	// Object storage for user-uploaded artifacts (avatars, etc.).
	Storage storage.Store

	// Encrypted-DEK store. Served to workers over HTTPS at
	// /api/v1/internal/dek/:userID so workers don't need direct Postgres
	// access. Backend processes use Postgres directly; workers use the
	// HTTP-proxy implementation.
	EncryptedKeys encryptedkeys.Store

	// Worker messageId -> internal email map, served to workers over HTTPS at
	// /api/v1/internal/email-message-map for the same no-direct-Postgres reason
	// as EncryptedKeys. Backed by Postgres in the backend.
	EmailMessageMap repository.EmailMessageMapRepository

	// Direct repositories used by handlers that don't yet have a
	// service layer (avatars, etc.). Keep narrow and add a service
	// only when business logic accumulates.
	UserRepo                 repository.UserRepository
	OrgRepo                  repository.OrganizationRepository
	StorageBackendRepo       repository.StorageBackendRepository
	CloudCredentialRepo      repository.CloudCredentialRepository
	ProvisioningTemplateRepo repository.ProvisioningTemplateRepository
	ProvisioningJobRepo      repository.ProvisioningJobRepository
	ProvisioningPolicyRepo   repository.ProvisioningPolicyRepository

	// Danger zone (delayed deletions for orgs & user accounts)
	DangerZoneService dangerzone.Service
}
