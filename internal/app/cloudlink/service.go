// Package cloudlink is the self-hosted side of the warmup pool link.
package cloudlink

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const DefaultCloudURL = "https://api.warmbly.com"

var (
	ErrNotConnected    = errx.NewWithIdentifier(errx.Conflict, "cloud_link_not_connected", "This instance is not connected to Warmbly Cloud.")
	ErrAlreadyLinked   = errx.NewWithIdentifier(errx.Conflict, "cloud_link_connected", "This instance is already connected. Disconnect first to link a different workspace.")
	ErrNoPendingCode   = errx.NewWithIdentifier(errx.NotFound, "cloud_link_no_pending", "No connection in progress. Start again.")
	ErrCodeExpired     = errx.NewWithIdentifier(errx.NotFound, "cloud_link_code_expired", "The code expired before it was approved. Start again.")
	ErrOAuthMailbox    = errx.NewWithIdentifier(errx.Unprocessable, "cloud_link_oauth_mailbox", "Google and Microsoft sign-in mailboxes cannot be warmed by Warmbly Cloud yet, because their refresh grant is bound to this instance's own OAuth app. Connect the mailbox with SMTP/IMAP (an app password) to enroll it.")
	ErrMailboxInactive = errx.NewWithIdentifier(errx.Unprocessable, "cloud_link_mailbox_inactive", "Only active mailboxes can be enrolled.")
)

// cloudURLAllowed requires TLS: the token and mailbox passwords travel on
// this URL. Loopback is exempt for local development.
func cloudURLAllowed(u string) bool {
	if strings.HasPrefix(u, "https://") {
		return true
	}
	if !strings.HasPrefix(u, "http://") {
		return false
	}
	host := strings.TrimPrefix(u, "http://")
	if i := strings.IndexAny(host, ":/"); i >= 0 {
		host = host[:i]
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// CloudURL is where the instance links to: WARMBLY_CLOUD_URL or the hosted API.
func CloudURL() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("WARMBLY_CLOUD_URL")), "/"); v != "" {
		return v
	}
	return DefaultCloudURL
}

func instanceVersion() string {
	if v := strings.TrimSpace(os.Getenv("WARMBLY_VERSION")); v != "" {
		return v
	}
	return "dev"
}

func instanceName() string {
	if v := strings.TrimSpace(os.Getenv("INSTANCE_NAME")); v != "" {
		return v
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "Self-hosted Warmbly"
}

// PendingConnect is an in-flight device-code handshake, held in memory.
type PendingConnect struct {
	DeviceCode      string    `json:"-"`
	UserCode        string    `json:"user_code"`
	VerificationURL string    `json:"verification_url"`
	CloudURL        string    `json:"cloud_url"`
	ExpiresAt       time.Time `json:"expires_at"`
	Interval        int       `json:"interval"`
	StartedBy       uuid.UUID `json:"-"`
}

// ConnectPollResult is the dashboard's poll answer.
type ConnectPollResult struct {
	Status models.PoolLinkCodeStatus    `json:"status"`
	Link   *models.CloudLink            `json:"link,omitempty"`
	Info   *models.PoolLinkInstanceInfo `json:"info,omitempty"`
}

type Service interface {
	Status(ctx context.Context) (*models.CloudLinkStatus, *errx.Error)
	StartConnect(ctx context.Context, userID uuid.UUID, cloudURL string) (*PendingConnect, *errx.Error)
	PollConnect(ctx context.Context, userID uuid.UUID) (*ConnectPollResult, *errx.Error)
	Disconnect(ctx context.Context) *errx.Error

	ListMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.CloudLinkMailboxRow, *errx.Error)
	Enroll(ctx context.Context, orgID, accountID uuid.UUID) (*models.CloudLinkMailboxRow, *errx.Error)
	Unenroll(ctx context.Context, orgID, accountID uuid.UUID) *errx.Error
	SetLifecycle(ctx context.Context, orgID, accountID uuid.UUID, action string) (*models.CloudLinkMailboxRow, *errx.Error)

	// Cloud-managed mailboxes: consent through the cloud, tokens brokered from it (managed.go).
	StartOAuth(ctx context.Context, orgID, userID uuid.UUID, provider models.InboxProvider) (*models.CloudLinkOAuthStart, *errx.Error)
	FinishOAuth(ctx context.Context, orgID, userID uuid.UUID, session string) (*models.Email, *errx.Error)
	ListWorkspaceMailboxes(ctx context.Context) ([]models.PoolLinkWorkspaceMailbox, *errx.Error)
	Adopt(ctx context.Context, orgID, userID, cloudAccountID uuid.UUID) (*models.Email, *errx.Error)
	// AccessToken is the worker's credential for a managed mailbox, via the internal API.
	AccessToken(ctx context.Context, accountID uuid.UUID) (*models.PoolLinkAccessToken, *errx.Error)

	// IsEnrolled is the local warmup scheduler's stand-down check; fails closed to false.
	IsEnrolled(ctx context.Context, accountID uuid.UUID) bool
	// VerifyWarmupToken is the consumer's check that warmup mail in an enrolled mailbox is the cloud's.
	VerifyWarmupToken(ctx context.Context, accountID uuid.UUID, token string) (bool, error)
}

type service struct {
	repo     repository.CloudLinkRepository
	emails   repository.EmailRepository
	emailSvc email.EmailService

	mu       sync.Mutex
	pending  *PendingConnect
	sessions map[string]oauthSession
	tokens   map[uuid.UUID]cachedToken
}

func NewService(repo repository.CloudLinkRepository, emails repository.EmailRepository, emailSvc email.EmailService) Service {
	return &service{repo: repo, emails: emails, emailSvc: emailSvc, sessions: map[string]oauthSession{}, tokens: map[uuid.UUID]cachedToken{}}
}

func (s *service) link(ctx context.Context) (*models.CloudLink, *errx.Error) {
	l, err := s.repo.Get(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	if l == nil {
		return nil, ErrNotConnected
	}
	return l, nil
}

func (s *service) clientFor(l *models.CloudLink) *client {
	return newClient(l.CloudURL, l.Token, instanceVersion())
}

func (s *service) Status(ctx context.Context) (*models.CloudLinkStatus, *errx.Error) {
	st := &models.CloudLinkStatus{DefaultCloudURL: CloudURL()}
	l, err := s.repo.Get(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	if l == nil {
		return st, nil
	}
	st.Connected = true
	st.Link = l
	var info models.PoolLinkInstanceInfo
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance", nil, &info); xerr != nil {
		st.Error = xerr.Message
		_ = s.repo.SetSyncResult(ctx, time.Now(), xerr.Message)
		return st, nil
	}
	st.Reachable = true
	st.Info = &info
	_ = s.repo.SetSyncResult(ctx, time.Now(), "")
	return st, nil
}

func (s *service) StartConnect(ctx context.Context, userID uuid.UUID, cloudURL string) (*PendingConnect, *errx.Error) {
	if l, err := s.repo.Get(ctx); err == nil && l != nil {
		return nil, ErrAlreadyLinked
	}
	cloudURL = strings.TrimRight(strings.TrimSpace(cloudURL), "/")
	if cloudURL == "" {
		cloudURL = CloudURL()
	}
	if !cloudURLAllowed(cloudURL) {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "cloud_link_url", "The cloud URL must start with https://.")
	}
	c := newClient(cloudURL, "", instanceVersion())
	var res models.PoolLinkStartResponse
	if xerr := c.do(ctx, http.MethodPost, "/codes", models.PoolLinkStartRequest{
		InstanceName:    instanceName(),
		InstanceURL:     config.AppBaseURL(),
		InstanceVersion: instanceVersion(),
	}, &res); xerr != nil {
		return nil, xerr
	}
	p := &PendingConnect{
		DeviceCode:      res.DeviceCode,
		UserCode:        res.UserCode,
		VerificationURL: res.VerificationURL,
		CloudURL:        cloudURL,
		ExpiresAt:       time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
		Interval:        res.Interval,
		StartedBy:       userID,
	}
	s.mu.Lock()
	s.pending = p
	s.mu.Unlock()
	return p, nil
}

func (s *service) PollConnect(ctx context.Context, userID uuid.UUID) (*ConnectPollResult, *errx.Error) {
	s.mu.Lock()
	p := s.pending
	s.mu.Unlock()
	if p == nil {
		// Another tab may have finished the handshake already.
		if l, err := s.repo.Get(ctx); err == nil && l != nil {
			return &ConnectPollResult{Status: models.PoolLinkCodeApproved, Link: l}, nil
		}
		return nil, ErrNoPendingCode
	}
	if time.Now().After(p.ExpiresAt) {
		s.clearPending(p)
		return nil, ErrCodeExpired
	}
	c := newClient(p.CloudURL, "", instanceVersion())
	var res models.PoolLinkPollResponse
	if xerr := c.do(ctx, http.MethodPost, "/poll", map[string]string{"device_code": p.DeviceCode}, &res); xerr != nil {
		if xerr.Identifier == "pool_link_denied" || xerr.Identifier == "pool_link_code_not_found" || xerr.Identifier == "pool_link_code_used" {
			s.clearPending(p)
		}
		return nil, xerr
	}
	if res.Status != models.PoolLinkCodeApproved {
		return &ConnectPollResult{Status: models.PoolLinkCodePending}, nil
	}
	if res.InstanceID == nil || res.InstanceToken == "" {
		return nil, errx.InternalError()
	}
	l := &models.CloudLink{
		CloudURL:    p.CloudURL,
		InstanceID:  *res.InstanceID,
		Token:       res.InstanceToken,
		ConnectedBy: &userID,
	}
	if res.Organization != nil {
		l.OrganizationName = res.Organization.Name
	}
	if err := s.repo.Put(ctx, l); err != nil {
		return nil, errx.InternalError()
	}
	s.clearPending(p)
	var info models.PoolLinkInstanceInfo
	out := &ConnectPollResult{Status: models.PoolLinkCodeApproved, Link: l}
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance", nil, &info); xerr == nil {
		out.Info = &info
	}
	return out, nil
}

func (s *service) clearPending(p *PendingConnect) {
	s.mu.Lock()
	if s.pending == p {
		s.pending = nil
	}
	s.mu.Unlock()
}

// linkAlreadyGone: the cloud no longer recognises the instance, so nothing is left to release there.
func linkAlreadyGone(xerr *errx.Error) bool {
	switch xerr.Identifier {
	case "pool_link_revoked", "pool_link_instance_not_found", "unauthorized":
		return true
	}
	return false
}

func (s *service) Disconnect(ctx context.Context) *errx.Error {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return xerr
	}
	// The cloud must confirm (or already have dropped) the link before local
	// state goes, or managed mailboxes stay owned there with no way to retry.
	if xerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance", nil, nil); xerr != nil && !linkAlreadyGone(xerr) {
		return xerr
	}
	// Managed mirrors have no credential of their own; they end with the link.
	if rows, err := s.repo.List(ctx); err == nil {
		for _, m := range rows {
			if !m.Managed || s.emailSvc == nil {
				continue
			}
			if acc, xerr := s.emails.GetByID(ctx, m.EmailAccountID); xerr == nil {
				s.forgetToken(m.EmailAccountID)
				_ = s.emailSvc.Delete(ctx, acc.UserID, acc.ID.String())
			}
		}
	}
	if err := s.repo.UnenrollAll(ctx); err != nil {
		return errx.InternalError()
	}
	if err := s.repo.Delete(ctx); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) IsEnrolled(ctx context.Context, accountID uuid.UUID) bool {
	ok, err := s.repo.IsEnrolled(ctx, accountID)
	return err == nil && ok
}

func (s *service) ListMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.CloudLinkMailboxRow, *errx.Error) {
	accounts, xerr := s.emails.GetAllActiveInScope(ctx, repository.NewAccountScope(&orgID))
	if xerr != nil {
		return nil, xerr
	}
	enrolled, err := s.repo.List(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	byAccount := make(map[uuid.UUID]models.CloudLinkMailbox, len(enrolled))
	for _, e := range enrolled {
		byAccount[e.EmailAccountID] = e
	}

	// One round trip for every enrolled mailbox's cloud state.
	cloudByRemote := map[uuid.UUID]*models.PoolLinkMailboxState{}
	if len(enrolled) > 0 {
		if l, err := s.repo.Get(ctx); err == nil && l != nil {
			var states []models.PoolLinkMailboxState
			if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/mailboxes", nil, &states); xerr == nil {
				for i := range states {
					cloudByRemote[states[i].RemoteID] = &states[i]
				}
			}
		}
	}

	rows := make([]models.CloudLinkMailboxRow, 0, len(accounts))
	for _, a := range accounts {
		row := models.CloudLinkMailboxRow{ID: a.ID, Email: a.Email, Name: a.Name, Provider: a.Provider, Status: a.Status}
		if e, ok := byAccount[a.ID]; ok {
			at := e.EnrolledAt
			row.Enrolled = true
			row.EnrolledAt = &at
			row.Managed = e.Managed
			row.Cloud = cloudByRemote[e.RemoteID]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *service) row(ctx context.Context, orgID, accountID uuid.UUID) (*models.CloudLinkMailboxRow, *errx.Error) {
	rows, xerr := s.ListMailboxes(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	for i := range rows {
		if rows[i].ID == accountID {
			return &rows[i], nil
		}
	}
	return nil, errx.ErrNotFound
}

func (s *service) Enroll(ctx context.Context, orgID, accountID uuid.UUID) (*models.CloudLinkMailboxRow, *errx.Error) {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	acc, xerr := s.emails.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	if acc.OrganizationID == nil || *acc.OrganizationID != orgID {
		return nil, errx.ErrNotFound
	}
	if acc.Status != "active" {
		return nil, ErrMailboxInactive
	}
	req := models.PoolLinkEnrollRequest{
		RemoteID: acc.ID,
		Email:    acc.Email,
		Name:     acc.Name,
		Provider: models.InboxProvider(acc.Provider),
		Warmup: models.PoolLinkWarmupSettings{
			Base: acc.WarmupBase, Max: acc.WarmupMax, Increase: acc.WarmupIncrease, ReplyRate: acc.WarmupReplyRate,
			StartTime: acc.WarmupStartTime, EndTime: acc.WarmupEndTime, Days: acc.WarmupDays, Timezone: acc.Timezone,
		},
	}
	switch req.Provider {
	case models.InboxProviderSMTPIMAP:
		creds, xerr := s.emails.GetSMTPCredentials(ctx, acc.ID)
		if xerr != nil {
			return nil, xerr
		}
		req.SMTPIMAP = &models.SmtpImap{
			SMTP: &models.Service{Host: creds.SMTPHost, Port: creds.SMTPPort, Username: creds.SMTPUser, Password: creds.SMTPPassword, Security: creds.SMTPSecurity},
			IMAP: &models.Service{Host: creds.IMAPHost, Port: creds.IMAPPort, Username: creds.IMAPUser, Password: creds.IMAPPassword, Security: creds.IMAPSecurity},
		}
	default:
		return nil, ErrOAuthMailbox
	}

	var state models.PoolLinkMailboxState
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/mailboxes", req, &state); xerr != nil {
		return nil, xerr
	}
	if _, err := s.repo.Enroll(ctx, acc.ID, acc.ID, false); err != nil {
		// Without the local row the mailbox would warm in both places; undo the cloud side.
		if xerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+acc.ID.String(), nil, nil); xerr != nil {
			log.Error().Str("account_id", acc.ID.String()).Str("code", xerr.Identifier).Msg("cloud link: local enrollment failed and the cloud copy could not be removed; unenroll it from Settings")
		}
		return nil, errx.InternalError()
	}
	return s.row(ctx, orgID, accountID)
}

func (s *service) Unenroll(ctx context.Context, orgID, accountID uuid.UUID) *errx.Error {
	acc, xerr := s.emails.GetByID(ctx, accountID)
	if xerr != nil {
		return xerr
	}
	if acc.OrganizationID == nil || *acc.OrganizationID != orgID {
		return errx.ErrNotFound
	}
	m, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil {
		return errx.InternalError()
	}
	if m == nil {
		return nil
	}
	if m.Managed {
		return s.removeManaged(ctx, acc.UserID, m)
	}
	// Local row first, so a failed cloud call can be retried from a consistent
	// state instead of leaving the mailbox with no warmup anywhere.
	if err := s.repo.Unenroll(ctx, accountID); err != nil {
		return errx.InternalError()
	}
	if l, err := s.repo.Get(ctx); err == nil && l != nil {
		if xerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+m.RemoteID.String(), nil, nil); xerr != nil && xerr.Identifier != "pool_link_mailbox_not_found" {
			if _, rerr := s.repo.Enroll(ctx, accountID, m.RemoteID, false); rerr != nil {
				log.Error().Str("account_id", accountID.String()).Msg("cloud link: cloud unenroll failed and the local row could not be restored")
			}
			return xerr
		}
	}
	return nil
}

func (s *service) SetLifecycle(ctx context.Context, orgID, accountID uuid.UUID, action string) (*models.CloudLinkMailboxRow, *errx.Error) {
	if action != "pause" && action != "resume" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "cloud_link_lifecycle", "Action must be pause or resume.")
	}
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	m, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil {
		return nil, errx.ErrNotFound
	}
	var state models.PoolLinkMailboxState
	if xerr := s.clientFor(l).do(ctx, http.MethodPatch, "/instance/mailboxes/"+m.RemoteID.String(), models.PoolLinkMailboxPatch{Lifecycle: action}, &state); xerr != nil {
		return nil, xerr
	}
	return s.row(ctx, orgID, accountID)
}
