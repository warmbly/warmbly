package email

import (
	"context"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"golang.org/x/oauth2"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/feature"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/dnsauth"
	"github.com/warmbly/warmbly/internal/repository"
)

type EmailService interface {
	Search(ctx context.Context, userID, search, cursor, tag, limit string, allowedAccountIDs []uuid.UUID) (*models.EmailsResult, *errx.Error)
	Get(ctx context.Context, userID, emailAccountID string) (*models.Email, *errx.Error)
	Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	// BulkUpdateTags adds/removes tags across many of the user's mailboxes
	// in one call; returns how many of the requested mailboxes were owned.
	BulkUpdateTags(ctx context.Context, userID string, emailIDs, addTags, removeTags []uuid.UUID) (int, *errx.Error)
	// SetWarmupLifecycle starts, pauses, resumes, or disables warmup for a
	// mailbox. start/resume preserve ramp progress; disable turns warmup off.
	SetWarmupLifecycle(ctx context.Context, userID, emailAccountID, action string) (*models.Email, *errx.Error)
	// SetSendHold holds a mailbox in reserve or releases it; a release lands
	// wherever its warmup health says, so an unhealthy mailbox rests.
	SetSendHold(ctx context.Context, orgID, emailAccountID string, hold bool) (*models.SendLifecycleState, *errx.Error)
	// UpdateTrackingDomain sets or clears the custom open/click tracking
	// domain and resolves it once, persisting the verdict.
	UpdateTrackingDomain(ctx context.Context, orgID, emailAccountID, domain string) (*models.TrackingDomainStatus, *errx.Error)
	// GetTrackingDomain reports the stored state plus the CNAME target this
	// install expects. Read-only: it does no DNS work.
	GetTrackingDomain(ctx context.Context, orgID, emailAccountID string) (*models.TrackingDomainStatus, *errx.Error)
	// VerifyTrackingDomain re-resolves the stored domain and PERSISTS the
	// verdict, which is what lets a fixed record start routing links. Same
	// read/write split as CheckDomainAuth and RefreshDomainAuth.
	VerifyTrackingDomain(ctx context.Context, orgID, emailAccountID string) (*models.TrackingDomainStatus, *errx.Error)
	// StartTrackingDomainSweep re-resolves every custom tracking domain on a
	// schedule, so a record that propagates starts being used without anybody
	// pressing anything, and one that disappears stops being used at all.
	StartTrackingDomainSweep(ctx context.Context, interval, staleAfter time.Duration)
	// CheckDomainAuth runs a live SPF/DKIM/DMARC lookup for a mailbox's
	// sending domain and returns it without touching stored state.
	CheckDomainAuth(ctx context.Context, orgID, emailAccountID string) (*dnsauth.Result, *errx.Error)
	// RefreshDomainAuth does the same and PERSISTS the verdict. That write can
	// lift the cold-send and warmup gate, so it sits behind the write
	// permission while CheckDomainAuth stays readable.
	RefreshDomainAuth(ctx context.Context, orgID, emailAccountID string) (*dnsauth.Result, *errx.Error)
	Delete(ctx context.Context, userID, emailAccountID string) *errx.Error

	// Onboarding flow. OAuthFinish's second return is true when the round
	// trip renewed an existing mailbox (OAuthReauth) rather than connecting
	// a new one, so the handler can audit and answer accordingly.
	OAuthStart(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider) (*models.EmailOnboardingStartResponse, *errx.Error)
	OAuthFinish(ctx context.Context, userID, code, state string) (*models.Email, bool, *errx.Error)
	OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)
	// OnboardSMTPIMAPBulk connects many SMTP/IMAP mailboxes in one call and
	// answers per row, so one bad password never fails the file. Rows past the
	// workspace's allowance are refused before any credential is dialled.
	OnboardSMTPIMAPBulk(ctx context.Context, userID string, orgID *uuid.UUID, rows []models.NewSMTPIMAPAccount) *models.MailboxBulkResult
	// OAuthReauth starts an OAuth round trip that renews the tokens of an
	// existing Gmail/Outlook mailbox after the provider invalidated them.
	OAuthReauth(ctx context.Context, userID string, orgID *uuid.UUID, accountID uuid.UUID) (*models.EmailOnboardingStartResponse, *errx.Error)
	// UpdateSMTPIMAPCredentials validates replacement credentials against a
	// live worker, stores them, and puts the mailbox back to work.
	UpdateSMTPIMAPCredentials(ctx context.Context, orgID *uuid.UUID, accountID uuid.UUID, creds *models.SmtpImap) (*models.Email, *errx.Error)

	// Optional: wire in the webhook dispatcher after construction. Once
	// set, account-lifecycle events fan out to customer webhook endpoints.
	WireWebhooks(w webhook.Service)
	// WireMailboxAllowance attaches the allowance resolver every connect path
	// checks. Without it the feature gate's free-or-paid split stands in.
	WireMailboxAllowance(src MailboxAllowanceSource)
	// WireGraphDelta attaches the Graph delta-cursor repository so the worker
	// reconciler can seed a mailbox's saved cursors when loading it.
	WireGraphDelta(repo repository.EmailGraphDeltaRepository)
	// WireEmailHistoryID attaches the Gmail history-cursor repository, the
	// Google counterpart of WireGraphDelta, so a reloaded mailbox resumes from
	// its saved checkpoint instead of re-bootstrapping.
	WireEmailHistoryID(repo repository.EmailHistoryIDRepository)
	// WireSyncState, WireMailboxes and WireSyncBudget feed the sync fair-use
	// payload: resumable backfill state, saved IMAP folder cursors, and the
	// operator-editable budget the mailbox syncs under.
	WireSyncState(repo repository.EmailSyncStateRepository)
	WireMailboxes(repo repository.MailboxRepository)
	WireSyncBudget(src SyncBudgetSource)
	WirePoolLink(repo repository.PoolLinkRepository)
	// WireCloudLink marks managed mailboxes, which ship to the worker without a credential.
	WireCloudLink(repo repository.CloudLinkRepository)
	// WireAccountErrors lets a successful reconnect resolve the credential
	// errors it just fixed, which is what clears the mailbox's error banner.
	WireAccountErrors(repo repository.EmailAccountErrorRepository)
	// Brokered OAuth (cloud side): consent on this deployment's OAuth app for a linked instance.
	OAuthAuthorizeURL(provider models.InboxProvider, state string) (string, *errx.Error)
	OAuthConnectWithCode(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider, code string) (*models.Email, *errx.Error)
	// OAuthAccessToken is a live access token for an OAuth mailbox, refreshed when near expiry.
	OAuthAccessToken(ctx context.Context, accountID uuid.UUID) (*oauth2.Token, *errx.Error)
	// LoadAccountOntoWorker assigns a worker if needed and ships the mailbox
	// to it (idempotent; the reconciler calls it too).
	LoadAccountOntoWorker(ctx context.Context, accountID uuid.UUID) error
	// GetSyncState is the dashboard's view of a mailbox's sync: nil state when
	// the worker has not reported yet.
	GetSyncState(ctx context.Context, userID, emailID string) (*models.SyncState, models.SyncPolicy, *errx.Error)
	// StartWorkerReconciler periodically ensures every active mailbox is
	// assigned to a worker and loaded onto it (blocks until ctx is cancelled).
	StartWorkerReconciler(ctx context.Context, interval time.Duration)
	// ReloadWorkerAccounts re-ships every active mailbox assigned to one
	// worker, for a worker that just booted and holds none in memory.
	ReloadWorkerAccounts(ctx context.Context, workerID uuid.UUID)
}

type emailService struct {
	emailRepository    repository.EmailRepository
	cipherService      cipher.CipherService
	featureGate        feature.FeatureGateService
	warmupService      warmupapp.Service
	publisher          events.Publisher
	streamingPublisher *pubsub.StreamingPublisher
	r                  *cache.Cache
	oauthInbox         *config.Oauth2Inbox
	workerAssignment   worker.WorkerAssignmentService
	allowance          MailboxAllowanceSource
	graphDelta         repository.EmailGraphDeltaRepository
	historyID          repository.EmailHistoryIDRepository
	syncState          repository.EmailSyncStateRepository
	mailboxes          repository.MailboxRepository
	syncBudget         SyncBudgetSource
	// poolLink marks linked warmup-only mailboxes, which sync with no history.
	poolLink repository.PoolLinkRepository
	// cloudLink marks managed mailboxes whose credential the cloud holds.
	cloudLink repository.CloudLinkRepository
	// webhookService is optional. When non-nil, account lifecycle events
	// (email_account.connected, email_account.removed) are dispatched to
	// subscribed customer webhooks.
	webhookService webhook.Service
	// orgRiskRepo bars a restricted organization from the paid warmup pool.
	// Optional/nil-safe.
	orgRiskRepo repository.OrgRiskRepository
	// lifecycleRepo backs the owner's hold; without it SetSendHold refuses.
	lifecycleRepo repository.SendLifecycleRepository
	// accountErrors is resolved-on-reconnect error state. Optional/nil-safe.
	accountErrors repository.EmailAccountErrorRepository
}

// WireAccountErrors attaches the mailbox error log so reconnects can resolve it.
func (s *emailService) WireAccountErrors(repo repository.EmailAccountErrorRepository) {
	s.accountErrors = repo
}

// WireLifecycle attaches the cold-sending lifecycle.
func (s *emailService) WireLifecycle(r repository.SendLifecycleRepository) {
	s.lifecycleRepo = r
}

// LifecycleAware is the optional capability the caller uses to attach it.
type LifecycleAware interface {
	WireLifecycle(r repository.SendLifecycleRepository)
}

// WireOrgRisk attaches the organization risk posture.
func (s *emailService) WireOrgRisk(r repository.OrgRiskRepository) {
	s.orgRiskRepo = r
}

// OrgRiskAware is the optional capability the caller uses to attach org risk.
type OrgRiskAware interface {
	WireOrgRisk(r repository.OrgRiskRepository)
}

// main.go attaches the posture by type assertion, which fails silently.
var _ OrgRiskAware = (*emailService)(nil)

// SyncBudgetSource is the operator-editable sync fair-use section, satisfied
// by instancesettings.Service. Injected post-construction; when unset the
// loader ships compiled defaults.
type SyncBudgetSource interface {
	SyncBudget(ctx context.Context) instancesettings.Sync
}

// WireSyncState attaches the mailbox sync-state repository so a (re)loaded
// mailbox resumes its backfill and the API can report progress.
func (s *emailService) WireSyncState(repo repository.EmailSyncStateRepository) {
	s.syncState = repo
}

// WireMailboxes attaches the IMAP folder-state repository so a reloaded IMAP
// mailbox resumes incrementally from its saved HIGHESTMODSEQ per folder
// instead of re-walking every folder from scratch.
func (s *emailService) WireMailboxes(repo repository.MailboxRepository) {
	s.mailboxes = repo
}

// WireSyncBudget attaches the instance settings the sync policy is read from.
func (s *emailService) WireSyncBudget(src SyncBudgetSource) {
	s.syncBudget = src
}

// WirePoolLink attaches the pool-link repository so linked mailboxes get the
// warmup-only sync policy.
func (s *emailService) WireCloudLink(repo repository.CloudLinkRepository) {
	s.cloudLink = repo
}

func (s *emailService) WirePoolLink(repo repository.PoolLinkRepository) {
	s.poolLink = repo
}

// MailboxAllowanceSource answers how many mailboxes a workspace may hold.
// Satisfied by the organization service; injected post-construction so this
// package needs no import of it.
type MailboxAllowanceSource interface {
	MailboxAllowance(ctx context.Context, orgID uuid.UUID) (*models.MailboxAllowance, *errx.Error)
}

func (s *emailService) WireMailboxAllowance(src MailboxAllowanceSource) {
	s.allowance = src
}

// WireWebhooks attaches the webhook dispatcher after construction. Done
// post-construction so callers without a webhook stack (tests, jobs) need
// not provide one.
func (s *emailService) WireWebhooks(w webhook.Service) {
	s.webhookService = w
}

func NewService(
	emailRepository repository.EmailRepository,
	cipherService cipher.CipherService,
	featureGate feature.FeatureGateService,
	warmupService warmupapp.Service,
	publisher events.Publisher,
	streamingPublisher ...*pubsub.StreamingPublisher,
) EmailService {
	var realtime *pubsub.StreamingPublisher
	if len(streamingPublisher) > 0 {
		realtime = streamingPublisher[0]
	}

	return &emailService{
		emailRepository:    emailRepository,
		cipherService:      cipherService,
		featureGate:        featureGate,
		warmupService:      warmupService,
		publisher:          publisher,
		streamingPublisher: realtime,
	}
}

// NewServiceWithWorker builds the email service with the deps needed for
// worker-facing flows (credential validation over the event bus, mailbox OAuth,
// worker assignment). Publishing goes through the events.Publisher, so this is
// transport-agnostic (Kafka or NATS).
func NewServiceWithWorker(
	emailRepository repository.EmailRepository,
	cipherService cipher.CipherService,
	featureGate feature.FeatureGateService,
	warmupService warmupapp.Service,
	publisher events.Publisher,
	r *cache.Cache,
	oauthInbox *config.Oauth2Inbox,
	workerAssignment worker.WorkerAssignmentService,
	streamingPublisher ...*pubsub.StreamingPublisher,
) EmailService {
	var realtime *pubsub.StreamingPublisher
	if len(streamingPublisher) > 0 {
		realtime = streamingPublisher[0]
	}

	return &emailService{
		emailRepository:    emailRepository,
		cipherService:      cipherService,
		featureGate:        featureGate,
		warmupService:      warmupService,
		publisher:          publisher,
		streamingPublisher: realtime,
		r:                  r,
		oauthInbox:         oauthInbox,
		workerAssignment:   workerAssignment,
	}
}

func (s *emailService) publishAccountEvent(ctx context.Context, eventType pubsub.EventType, account *models.Email) {
	if s.streamingPublisher == nil || account == nil {
		return
	}

	var orgID string
	if account.OrganizationID != nil {
		orgID = account.OrganizationID.String()
	}
	s.streamingPublisher.PublishAccountEvent(ctx, &pubsub.AccountEvent{
		BaseEvent: pubsub.BaseEvent{
			EventType: eventType,
			UserID:    account.UserID,
		},
		OrgID:          orgID,
		EmailAccountID: account.ID.String(),
		Email:          account.Email,
		Provider:       account.Provider,
		Status:         account.Status,
	})
}

// GetSyncState returns the persisted sync state and the policy currently in
// force. It goes through Get so ownership is checked the same way as every
// other per-mailbox read.
func (s *emailService) GetSyncState(ctx context.Context, userID, emailID string) (*models.SyncState, models.SyncPolicy, *errx.Error) {
	acc, xerr := s.Get(ctx, userID, emailID)
	if xerr != nil {
		return nil, models.SyncPolicy{}, xerr
	}
	data := s.syncDataFor(ctx, acc.ID)
	return data.State, data.Policy, nil
}
