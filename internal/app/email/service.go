package email

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"golang.org/x/oauth2"

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
	// Update writes a mailbox's settings. orgID scopes the write (the mailbox
	// is a workspace asset); userID only names who to tell the worker about.
	Update(ctx context.Context, orgID, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	// BulkUpdateTags adds/removes tags across many of the workspace's
	// mailboxes in one call; returns how many of the requested mailboxes the
	// workspace owns.
	BulkUpdateTags(ctx context.Context, orgID string, emailIDs, addTags, removeTags []uuid.UUID) (int, *errx.Error)
	// SetWarmupLifecycle starts, pauses, resumes, or disables warmup for a
	// mailbox. start/resume preserve ramp progress; disable turns warmup off.
	SetWarmupLifecycle(ctx context.Context, orgID, emailAccountID, action string) (*models.Email, *errx.Error)
	// SetSendHold holds a mailbox in reserve or releases it; a release lands
	// wherever its warmup health says, so an unhealthy mailbox rests.
	SetSendHold(ctx context.Context, orgID, emailAccountID string, hold bool) (*models.SendLifecycleState, *errx.Error)
	// UpdateTrackingDomain sets or clears the custom open/click tracking
	// domain and resolves it once, persisting the verdict.
	UpdateTrackingDomain(ctx context.Context, orgID, emailAccountID, domain string) (*models.TrackingDomainStatus, *errx.Error)
	UpdateTrackDirectMail(ctx context.Context, orgID, emailAccountID string, enabled bool) *errx.Error
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
	Delete(ctx context.Context, orgID, emailAccountID string) *errx.Error

	// GetSendIdentity reports which addresses the mailbox's provider will let
	// it send as, which one is in use, and where the stored signature came
	// from. Read-only: it never calls the provider.
	GetSendIdentity(ctx context.Context, orgID, emailAccountID string) (*models.SendIdentity, *errx.Error)
	// RefreshSendIdentity re-reads that list from the provider and stores it,
	// importing the provider's signature too when asked. Gmail only; every
	// other provider is refused with mailbox_send_as_unsupported.
	RefreshSendIdentity(ctx context.Context, orgID, emailAccountID string, importSignature bool) (*models.SendIdentity, *errx.Error)

	// Onboarding flow. OAuthFinish's second return is true when the round
	// trip renewed an existing mailbox (OAuthReauth) rather than connecting
	// a new one, so the handler can audit and answer accordingly.
	// loginHint preselects an address in the provider's picker; "" for none.
	OAuthStart(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider, loginHint string) (*models.EmailOnboardingStartResponse, *errx.Error)
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
	WireUnibox(repo repository.UniboxRepository)
	WireSyncBudget(src SyncBudgetSource)
	// WireSeedScope lets the loader give a placement seed mailbox the sync
	// allowance a whole instance's test traffic needs.
	WireSeedScope(src SeedScopeSource)
	WirePoolLink(repo repository.PoolLinkRepository)
	// WireCloudLink marks managed mailboxes, which ship to the worker without a credential.
	WireCloudLink(repo repository.CloudLinkRepository)
	// WireCloudUnenroll attaches cloud credential revocation to mailbox deletion.
	WireCloudUnenroll(u CloudUnenroller)
	// ConnectDelegated stores a Gmail or Outlook mailbox reached through an
	// administrator's grant and loads it; tokens are minted per use.
	ConnectDelegated(ctx context.Context, userID string, orgID *uuid.UUID, data models.NewDelegatedAccount) (*models.Email, *errx.Error)
	// SwitchToAppPassword moves a per-mailbox Google sign-in onto an app password in place.
	SwitchToAppPassword(ctx context.Context, orgID *uuid.UUID, accountID uuid.UUID, appPassword string) (*models.Email, *errx.Error)
	// ReactivateDelegated puts a delegated mailbox back to work after its grant recovered.
	ReactivateDelegated(ctx context.Context, accountID uuid.UUID) (*models.Email, *errx.Error)
	// WireImportSignin lets an OAuth connect close the import rows that were
	// waiting for someone to sign in as that mailbox.
	WireImportSignin(r ImportSigninResolver)
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
	// SyncWarmupPool re-evaluates one mailbox's local warmup pool membership,
	// for a change outside the mailbox row (Warmbly Cloud enrollment).
	SyncWarmupPool(ctx context.Context, accountID uuid.UUID)
	// GetSyncState is the dashboard's view of a mailbox's sync: nil state when
	// the worker has not reported yet, the policy in force, and the folders
	// the sync has seen on the server so a client can name one to skip
	// (empty for providers without folders).
	GetSyncState(ctx context.Context, userID, emailID string) (*models.SyncState, models.SyncPolicy, []models.SyncFolder, *errx.Error)
	// UpdateSyncSettings replaces the mailbox's skip list, drops the mail
	// already stored from those folders, and re-ships the mailbox so the
	// worker applies it on its next pass. Returns the list as stored.
	UpdateSyncSettings(ctx context.Context, orgID, emailID string, body *models.UpdateSyncSettings) ([]string, *errx.Error)
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
	seedScope          SeedScopeSource
	// poolLink marks linked warmup-only mailboxes, which sync with no history.
	poolLink repository.PoolLinkRepository
	// cloudLink marks managed mailboxes whose credential the cloud holds.
	cloudLink repository.CloudLinkRepository
	// cloudUnenroll revokes a Warmbly Cloud enrollment on delete.
	cloudUnenroll CloudUnenroller
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
	// unibox is where a skipped folder's already-stored mail is dropped from.
	// Optional: without it the worker's retirement of the folder does it.
	unibox repository.UniboxRepository
	// importSignin closes import rows waiting on a sign-in. Optional.
	importSignin ImportSigninResolver
}

// WireUnibox attaches the unified inbox store, for the purge that follows a
// folder being excluded from sync.
func (s *emailService) WireUnibox(repo repository.UniboxRepository) {
	s.unibox = repo
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

// SeedScopeSource reports whether a mailbox is a placement seed.
type SeedScopeSource interface {
	SeedScope(ctx context.Context, accountID uuid.UUID) (string, error)
}

// WireSeedScope attaches the seed lookup the loader reads.
func (s *emailService) WireSeedScope(src SeedScopeSource) {
	s.seedScope = src
}

// WirePoolLink attaches the pool-link repository so linked mailboxes get the
// warmup-only sync policy.
func (s *emailService) WireCloudLink(repo repository.CloudLinkRepository) {
	s.cloudLink = repo
}

// CloudUnenroller confirms remote revocation before a mailbox is deleted locally.
type CloudUnenroller interface {
	RevokeForDelete(ctx context.Context, orgID, accountID uuid.UUID) *errx.Error
}

// WireCloudUnenroll attaches remote revocation after service construction.
func (s *emailService) WireCloudUnenroll(u CloudUnenroller) {
	s.cloudUnenroll = u
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
func (s *emailService) GetSyncState(ctx context.Context, orgID, emailID string) (*models.SyncState, models.SyncPolicy, []models.SyncFolder, *errx.Error) {
	acc, xerr := s.Get(ctx, orgID, emailID)
	if xerr != nil {
		return nil, models.SyncPolicy{}, nil, xerr
	}
	data, err := s.syncDataFor(ctx, acc.ID)
	if err != nil {
		log.Error().Err(err).Str("email_id", acc.ID.String()).Msg("sync state: policy lookup failed")
		return nil, models.SyncPolicy{}, nil, errx.InternalError()
	}
	return data.State, data.Policy, s.syncFoldersFor(ctx, acc), nil
}

// syncFoldersFor lists the IMAP folders the worker has reported for this
// mailbox, INBOX first and then by name, each with the canonical folder it
// files under. Gmail and Outlook mailboxes have no folder list here.
func (s *emailService) syncFoldersFor(ctx context.Context, acc *models.Email) []models.SyncFolder {
	out := []models.SyncFolder{}
	for _, box := range s.imapFoldersFor(ctx, acc) {
		out = append(out, models.SyncFolder{Name: box.Name, Folder: imap.CanonicalFolder(box)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := strings.EqualFold(out[i].Name, "INBOX"), strings.EqualFold(out[j].Name, "INBOX")
		if li != lj {
			return li
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// imapFoldersFor is the saved folder listing of an IMAP mailbox, and nothing
// for any other provider.
func (s *emailService) imapFoldersFor(ctx context.Context, acc *models.Email) []models.Mailbox {
	userID, err := uuid.Parse(acc.UserID)
	if acc.Provider != string(models.InboxProviderSMTPIMAP) || err != nil {
		return nil
	}
	return s.mailboxesFor(ctx, userID, acc.ID)
}

// UpdateSyncSettings stores a normalized skip list for an IMAP mailbox and
// drops the mail already stored from every saved folder the list covers,
// decided by the same matcher the worker applies (case, subfolders), plus
// the names themselves for a folder not listed yet. The re-ship makes the
// worker's next pass apply the list within a minute rather than at the
// reconciler's next republish; a folder it retires then is purged again by
// name, which is idempotent.
func (s *emailService) UpdateSyncSettings(ctx context.Context, orgID, emailID string, body *models.UpdateSyncSettings) ([]string, *errx.Error) {
	acc, xerr := s.Get(ctx, orgID, emailID)
	if xerr != nil {
		return nil, xerr
	}
	if body == nil {
		return nil, errx.ErrInvalid
	}
	if acc.Provider != string(models.InboxProviderSMTPIMAP) {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_sync_folder", "folders can only be skipped on an IMAP mailbox")
	}
	folders, xerr := imap.NormalizeSkipFolders(body.SkipFolders)
	if xerr != nil {
		return nil, xerr
	}
	// The purge reaches exactly what the worker will stop following: every
	// saved folder the matcher skips, plus a name no saved folder answers to
	// (a folder not listed yet). A name that IS a saved folder the matcher
	// refuses, by attribute, is refused here too rather than purged.
	saved := s.imapFoldersFor(ctx, acc)
	var purge []string
	for _, name := range folders {
		listed := false
		for _, box := range saved {
			if strings.EqualFold(box.Name, name) {
				listed = true
				if !imap.SkipsFolder(box, folders) {
					return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_sync_folder", fmt.Sprintf("folder %q is a folder the sync always follows", box.Name))
				}
			}
		}
		if !listed {
			purge = append(purge, name)
		}
	}
	for _, box := range saved {
		if imap.SkipsFolder(box, folders) && !slices.Contains(purge, box.Name) {
			purge = append(purge, box.Name)
		}
	}
	if xerr := s.emailRepository.SetSyncSkipFolders(ctx, orgID, emailID, folders); xerr != nil {
		return nil, xerr
	}
	if s.unibox != nil && len(purge) > 0 {
		if n, err := s.unibox.DeleteByFolderPaths(ctx, acc.ID, purge); err != nil {
			log.Warn().Err(err).Str("email_id", acc.ID.String()).Msg("sync skip folders: purge of stored mail failed; the worker retires the folders on its next pass")
		} else if n > 0 {
			log.Info().Str("email_id", acc.ID.String()).Int64("messages", n).Msg("sync skip folders: stored mail from skipped folders dropped")
		}
	}
	s.loadAccountBestEffort(ctx, acc.ID)
	return folders, nil
}
