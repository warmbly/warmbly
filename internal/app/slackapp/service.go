package slackapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/aiagent"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Stable error codes the dashboard branches on.
var (
	ErrSlackNotConfigured      = errx.NewWithIdentifier(errx.ServiceUnavailable, "slack_not_configured", "Slack is not set up on this Warmbly instance.")
	ErrSlackNotConnected       = errx.NewWithIdentifier(errx.NotFound, "slack_not_connected", "This workspace has not connected Slack.")
	ErrSlackLinkInvalid        = errx.NewWithIdentifier(errx.NotFound, "slack_link_invalid", "This link has expired or was already used. Ask Warmbly in Slack for a new one.")
	ErrSlackLinkEmailMismatch  = errx.NewWithIdentifier(errx.Forbidden, "slack_link_email_mismatch", "This Slack account's email is not the one you sign in to Warmbly with. Continue with Slack to confirm the account is yours.")
	ErrSlackVerifyUnavailable  = errx.NewWithIdentifier(errx.ServiceUnavailable, "slack_verify_unavailable", "Signing in with Slack is not set up on this Warmbly instance.")
	ErrSlackVerifyFailed       = errx.NewWithIdentifier(errx.Forbidden, "slack_verify_failed", "Slack could not confirm your account. Try again.")
	ErrSlackVerifyWrongAccount = errx.NewWithIdentifier(errx.Forbidden, "slack_verify_wrong_account", "You signed in to Slack as a different person than the one this link was made for. Sign in to Slack with that account and try again.")
	errSlackLinkNotMember      = errx.New(errx.Forbidden, "You are not a member of the Warmbly workspace this link belongs to.")
)

// Timeouts for work done after Slack has been answered.
const (
	agentRunTimeout = 10 * time.Minute
	shortTaskTime   = 30 * time.Second
	eventDedupeTTL  = 24 * time.Hour
	runLockTTL      = agentRunTimeout + time.Minute
	linkCodeTTL     = 15 * time.Minute
)

// Integrations is the integration service's Slack surface; the bot token
// stays sealed inside that package until SlackBotToken opens it.
type Integrations interface {
	ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error)
	SlackConnection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error)
	SlackConnectionsForTeam(ctx context.Context, teamID string) ([]models.IntegrationConnection, error)
	SlackBotToken(ctx context.Context, orgID, connID uuid.UUID) (string, error)
	SlackDefaultChannel(ctx context.Context, orgID uuid.UUID, conn *models.IntegrationConnection) string
	UpdateSlackSettings(ctx context.Context, orgID, connID uuid.UUID, settings models.SlackSettings) (*models.IntegrationConnection, error)
	MarkSlackTeamRevoked(ctx context.Context, teamID string, status models.IntegrationStatus, detail string) ([]uuid.UUID, error)
	SlackOAuthConfigured() bool
	SlackOAuthRedirectURL() string
	// SlackOAuthClient is the Slack app's client id and secret, for Sign in
	// with Slack.
	SlackOAuthClient() (clientID, clientSecret string)
}

// Organizations resolves workspaces and live membership.
type Organizations interface {
	Get(ctx context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error)
	GetMembership(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, *errx.Error)
}

// BanLookup reads a user's ban scope, so a login ban also ends Slack access.
type BanLookup interface {
	GetBanState(ctx context.Context, userID uuid.UUID) (uint32, error)
}

// AuditLogger records Slack-side link changes on the audit spine.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ip, userAgent string, changes, metadata map[string]string)
}

// CategoryEnsurer resolves an inbox label by title, creating the workspace's
// row on first use; satisfied by *repository.TagCategoryStore.
type CategoryEnsurer interface {
	EnsureCategory(ctx context.Context, orgID uuid.UUID, slug string) (uuid.UUID, error)
}

// PendingDrafts lists inbox-agent reply drafts; satisfied by repository.AIDraftRepository.
type PendingDrafts interface {
	ListPendingDrafts(ctx context.Context, orgID uuid.UUID, limit int) ([]models.AIThreadDraft, error)
}

// Deps are the Service's collaborators. Agent and Registry may be nil, which
// turns the assistant or the inbox actions off.
type Deps struct {
	Integrations Integrations
	Repo         repository.SlackRepository
	Orgs         Organizations
	Agent        aiagent.Service
	// Registry runs inbox actions through the same tool handlers the
	// dashboard agent uses, with their permission gates and audit.
	Registry  *aitools.Registry
	Audit     AuditLogger
	Redis     *redis.Client
	Threads   UniboxThreads
	Labels    CategoryEnsurer
	Drafts    PendingDrafts
	Users     UserLookup
	Bans      BanLookup
	Tasks     TaskLookup
	Campaigns CampaignLookup
	// Cipher seals drafts kept for "Review and send" with the org's DEK. Nil
	// keeps no drafts.
	Cipher cipher.CipherService
}

// Service is the Slack app: ingress handling, the assistant bridge, the
// inbox mirror's actions and the dashboard's Slack settings. It also notifies.
type Service struct {
	*Notifier
	inbox *InboxPoster

	orgs          Organizations
	bans          BanLookup
	agent         aiagent.Service
	registry      *aitools.Registry
	audit         AuditLogger
	threads       UniboxThreads
	labels        CategoryEnsurer
	drafts        PendingDrafts
	users         UserLookup
	cipher        cipher.CipherService
	guard         *guard
	signingSecret string

	botIDs   sync.Map // team id -> bot user id
	chanMu   sync.Mutex
	chanList map[uuid.UUID]cachedChannels
}

type cachedChannels struct {
	at   time.Time
	list []models.SlackChannel
}

// New builds the Slack app service. SLACK_SIGNING_SECRET is read once here.
func New(d Deps) *Service {
	return &Service{
		Notifier: NewNotifier(d.Integrations, d.Repo),
		inbox: NewInboxPoster(InboxDeps{
			Integrations: d.Integrations, Repo: d.Repo, Redis: d.Redis, Threads: d.Threads,
			Tasks: d.Tasks, Campaigns: d.Campaigns, Users: d.Users,
		}),
		orgs:          d.Orgs,
		bans:          d.Bans,
		agent:         d.Agent,
		registry:      d.Registry,
		audit:         d.Audit,
		threads:       d.Threads,
		labels:        d.Labels,
		drafts:        d.Drafts,
		users:         d.Users,
		cipher:        d.Cipher,
		guard:         newGuard(d.Redis),
		signingSecret: config.SlackSigningSecret(),
		chanList:      map[uuid.UUID]cachedChannels{},
	}
}

// ReplyQueued implements emailsend.ReplyObserver: a reply sent from Warmbly
// is mirrored into the conversation's Slack thread.
func (s *Service) ReplyQueued(ctx context.Context, orgID, userID uuid.UUID, threadID, body string, scheduledAt time.Time) {
	s.inbox.ReplyQueued(ctx, orgID, userID, threadID, body, scheduledAt)
}

// Interactive reports whether Slack requests can be verified and served.
func (s *Service) Interactive() bool { return s != nil && s.signingSecret != "" }

// Verify checks a Slack request signature against SLACK_SIGNING_SECRET.
func (s *Service) Verify(timestamp, signature string, body []byte) error {
	if !s.Interactive() {
		return ErrNoSigningSecret
	}
	return VerifySignature(s.signingSecret, timestamp, signature, body, time.Now())
}

// StartMaintenance purges expired link codes hourly until ctx ends.
func (s *Service) StartMaintenance(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(ctx, time.Minute)
				if _, err := s.repo.PurgeExpiredLinkCodes(pctx); err != nil {
					log.Warn().Err(err).Msg("slack: link code purge failed")
				}
				cancel()
			}
		}
	}()
}

// OnMemberRemoved drops the removed member's Slack link, for
// OrganizationService.WireMemberRemoval.
func (s *Service) OnMemberRemoved(ctx context.Context, orgID, userID uuid.UUID) error {
	_, err := s.repo.DeleteLinkForUser(ctx, orgID, userID)
	return err
}

// spawn runs fn after the Slack request has been answered, with its own
// deadline and panic recovery.
func (s *Service) spawn(name string, timeout time.Duration, fn func(ctx context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Str("task", name).Str("panic", fmt.Sprint(r)).Msg("slack: background task panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		fn(ctx)
	}()
}

// appURL makes a dashboard path absolute; "" when the instance has no APP_URL.
func appURL(path string) string {
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		return path
	}
	base := config.AppBaseURL()
	if base == "" || path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func sessionURL(sessionID uuid.UUID) string {
	return appURL("/app?agent_session=" + sessionID.String())
}

// settingsFrom reads SlackSettings out of a connection's config_capabilities.
func settingsFrom(conn *models.IntegrationConnection) models.SlackSettings {
	var st models.SlackSettings
	if conn != nil && len(conn.ConfigCapabilities) > 0 {
		_ = json.Unmarshal(conn.ConfigCapabilities, &st)
	}
	return st
}

// botUserID is the bot's own Slack user id in a team, cached per team.
func (s *Service) botUserID(ctx context.Context, teamID, token string) string {
	if v, ok := s.botIDs.Load(teamID); ok {
		return v.(string)
	}
	at, err := s.client.AuthTest(ctx, token)
	if err != nil || at.UserID == "" {
		return ""
	}
	s.botIDs.Store(teamID, at.UserID)
	return at.UserID
}

// memberState is the outcome of checking a link against live membership.
type memberState int

const (
	memberOK memberState = iota
	memberGone
	memberUnknown
	memberRestricted
)

// membership re-reads the linked member's org permissions; a link whose
// member left is deleted so it can never act again.
func (s *Service) membership(ctx context.Context, link *models.SlackUserLink) (*models.OrganizationMember, memberState) {
	m, xerr := s.orgs.GetMembership(ctx, link.OrganizationID, link.UserID)
	if xerr != nil {
		return nil, memberUnknown
	}
	if m == nil || m.AcceptedAt == nil {
		if _, err := s.repo.DeleteLinkBySlackUser(ctx, link.OrganizationID, link.SlackTeamID, link.SlackUserID); err != nil {
			log.Warn().Err(err).Msg("slack: dropping a stale link failed")
		}
		return nil, memberGone
	}
	if s.bans != nil {
		scope, err := s.bans.GetBanState(ctx, link.UserID)
		if err != nil || models.BanScope(scope).Has(models.BanScopeLogin) {
			return nil, memberUnknown
		}
	}
	// Slack mirrors the whole workspace's inbox, which a restricted member's grants do not cover.
	if m.IsRestricted() {
		return nil, memberRestricted
	}
	return m, memberOK
}
