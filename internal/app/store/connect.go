package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
)

// connectBudget bounds one auto-connect: a credential check dials the mail servers.
const connectBudget = 2 * time.Minute

// connectFailed is what a workspace reads when a purchased mailbox did not connect yet.
const connectFailed = "We could not connect this mailbox yet. We will keep trying."

// connectLater connects a mailbox that just became ready, off the caller's
// request: a webhook has to answer within seconds.
func (s *Service) connectLater(ctx context.Context, row *models.StoreMailbox, m *graphkit.Mailbox) {
	if !connectable(row, m) {
		return
	}
	s.connects.Add(1)
	go func() {
		defer s.connects.Done()
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectBudget)
		defer cancel()
		s.connect(cctx, row, m)
	}()
}

func connectable(row *models.StoreMailbox, m *graphkit.Mailbox) bool {
	return row.EmailAccountID == nil && m.Status == graphkit.MailboxActive && m.Connection.Ready && m.Connection.Method != graphkit.ConnectOAuth
}

func (s *Service) retryConnects(ctx context.Context) error {
	rows, err := s.Repo.PendingConnects(ctx, s.now(), 50)
	if err != nil {
		return err
	}
	for i := range rows {
		row := &rows[i]
		m, gerr := s.api().GetMailbox(ctx, row.GraphKitMailboxID)
		if gerr != nil {
			logUpstream("mailbox.refresh", gerr)
			// A definitive refusal spends an attempt, so retries back off and stop.
			if graphkit.Definitive(gerr) {
				if claimed, _ := s.Repo.ClaimConnect(ctx, row.OrganizationID, row.ID, s.now()); claimed {
					_ = s.Repo.FinishConnect(ctx, row.OrganizationID, row.ID, nil, models.StoreConnectFailed, connectFailed)
				}
			}
			continue
		}
		if fresh, err := s.upsertMailbox(ctx, row.OrganizationID, nil, m, 0); err == nil && fresh != nil {
			row = fresh
		}
		s.syncSignIn(ctx, row, m)
		if connectable(row, m) {
			cctx, cancel := context.WithTimeout(ctx, connectBudget)
			s.connect(cctx, row, m)
			cancel()
		}
	}
	return nil
}

// connect puts a purchased mailbox to work in its workspace with no prompt,
// warms it up with the workspace's defaults, and links it to the purchase.
func (s *Service) connect(ctx context.Context, row *models.StoreMailbox, m *graphkit.Mailbox) {
	orgID := row.OrganizationID
	claimed, err := s.Repo.ClaimConnect(ctx, orgID, row.ID, s.now())
	if err != nil || !claimed {
		return
	}
	s.publishMailbox(ctx, &models.StoreMailbox{ID: row.ID, OrganizationID: orgID, DomainID: row.DomainID, Status: row.Status, ConnectState: models.StoreConnectConnecting})
	userID := s.purchaser(ctx, row)
	name := displayname.FullName(row.FirstName, row.LastName)
	if name == "" {
		name = displayname.FromEmail(row.Email)
	}
	acc, cerr, waitReason := s.connectBy(ctx, orgID, userID, row, m, name)
	if waitReason != "" {
		// Not a failure: the domain's administrator approves the app once, and that wakes it (WakeMicrosoftConsent).
		_ = s.Repo.AwaitConnectAction(context.WithoutCancel(ctx), orgID, row.ID, models.ActionMicrosoftConsent, waitReason)
		s.publishMailbox(ctx, &models.StoreMailbox{ID: row.ID, OrganizationID: orgID, DomainID: row.DomainID, Status: row.Status, ConnectState: models.StoreConnectWaiting})
		return
	}
	if cerr != "" {
		_ = s.Repo.FinishConnect(context.WithoutCancel(ctx), orgID, row.ID, nil, models.StoreConnectFailed, cerr)
		s.publishMailbox(ctx, &models.StoreMailbox{ID: row.ID, OrganizationID: orgID, DomainID: row.DomainID, Status: row.Status, ConnectState: models.StoreConnectFailed})
		return
	}
	s.connected(ctx, row, acc)
}

// connected records a mailbox as connected to acc and puts it to work: warmup, photo, signature, tracking.
func (s *Service) connected(ctx context.Context, row *models.StoreMailbox, acc *models.Email) {
	orgID := row.OrganizationID
	if err := s.Repo.FinishConnect(context.WithoutCancel(ctx), orgID, row.ID, &acc.ID, models.StoreConnectConnected, ""); err != nil {
		log.Error().Err(err).Str("store_mailbox_id", row.ID.String()).Msg("store: recording the connected mailbox failed")
	}
	if s.Mailboxes != nil {
		// Starting warmup also anchors the graduation ceiling, so cold volume starts low and climbs on its own.
		// Warmup is a paid feature; a workspace without it keeps the mailbox connected and cold.
		if _, xerr := s.Mailboxes.SetWarmupLifecycle(ctx, orgID.String(), acc.ID.String(), "start"); xerr != nil {
			log.Info().Str("email_account_id", acc.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: warmup not started for a purchased mailbox")
		}
	}
	id := acc.ID
	s.publishMailbox(ctx, &models.StoreMailbox{ID: row.ID, OrganizationID: orgID, DomainID: row.DomainID, Status: row.Status,
		EmailAccountID: &id, ConnectState: models.StoreConnectConnected})
	if fresh, err := s.Repo.GetMailbox(ctx, orgID, row.ID); err == nil && fresh != nil {
		s.showPhotoOnConnect(ctx, fresh)
		s.applySignature(ctx, fresh)
	}
	if d, err := s.Repo.GetDomain(ctx, orgID, row.DomainID); err == nil && d != nil {
		s.trackMailbox(ctx, d, acc.ID)
	}
}

// connectBy connects through the mailbox's method and returns a workspace-safe failure, or why a Microsoft 365 tenant
// that has not approved this instance's app is left to its administrator, which is not a failure.
func (s *Service) connectBy(ctx context.Context, orgID, userID uuid.UUID, row *models.StoreMailbox, m *graphkit.Mailbox, name string) (*models.Email, string, string) {
	switch m.Connection.Method {
	case graphkit.ConnectOAuth:
		// Connects only through its sign-in link (signin.go), never here.
		return nil, connectFailed, ""
	case graphkit.ConnectToken, graphkit.ConnectDelegation:
		if s.Delegator == nil {
			return nil, connectFailed, ""
		}
		provider := models.GrantProviderGoogle
		if m.Provider == graphkit.ProviderMicrosoft {
			provider = models.GrantProviderMicrosoft
		}
		// A Google Workspace delegated to this instance's own service account is
		// read through its admin mailbox, like any administrator's grant.
		adminRow, err := s.Repo.DomainAdminMailbox(ctx, orgID, row.DomainID)
		if err != nil {
			return nil, connectFailed, ""
		}
		admin := row.Email
		if adminRow != nil {
			admin = adminRow.Email
		}
		brokered := m.Connection.Method == graphkit.ConnectToken
		acc, xerr := s.Delegator.ConnectPurchased(ctx, orgID, userID, provider, row.Email, name, admin, brokered)
		if xerr != nil && xerr.Identifier == delegation.ErrIDMicrosoftTenantConsent {
			if failure, reason := s.approveTenant(ctx, orgID, userID, row, adminRow); failure != "" || reason != "" {
				return nil, failure, reason
			}
			acc, xerr = s.Delegator.ConnectPurchased(ctx, orgID, userID, provider, row.Email, name, admin, brokered)
		}
		if xerr != nil {
			log.Warn().Str("store_mailbox_id", row.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: connecting a purchased mailbox failed")
			return nil, connectFailed, ""
		}
		return acc, "", ""
	case graphkit.ConnectPassword:
		acc, failure := s.connectPassword(ctx, orgID, userID, row, name)
		return acc, failure, ""
	}
	return nil, connectFailed, ""
}

// approveTenant approves the app in the mailbox's tenant with the admin mailbox's sign-in: nothing when done, a failure to
// retry soon, or why it is left to the administrator. A domain that fell back never signs in again, so nothing locks the account.
func (s *Service) approveTenant(ctx context.Context, orgID, userID uuid.UUID, row *models.StoreMailbox, admin *models.StoreMailbox) (failure, reason string) {
	if reason, err := s.Repo.DomainActionReason(ctx, orgID, row.DomainID, models.ActionMicrosoftConsent); err != nil {
		return connectFailed, ""
	} else if reason != "" {
		return "", reason
	}
	if admin == nil || admin.GraphKitMailboxID == "" {
		return "", models.ConsentReasonNoSignIn
	}
	creds, gerr := s.api().MailboxCredentials(ctx, admin.GraphKitMailboxID)
	if gerr != nil {
		logUpstream("mailbox.credentials", gerr)
		if graphkit.Definitive(gerr) {
			return "", models.ConsentReasonNoSignIn
		}
		return connectFailed, ""
	}
	username := creds.Username
	if username == "" {
		username = admin.Email
	}
	err := s.Delegator.ConsentPurchasedTenantWithAdmin(ctx, orgID, userID, row.Email[strings.LastIndex(row.Email, "@")+1:], username, creds.Password)
	creds.Password, creds.AppPassword = "", ""
	if err == nil {
		return "", ""
	}
	var ae *delegation.AutoConsentError
	if errors.As(err, &ae) && !ae.Retryable() {
		return "", ae.Reason
	}
	return connectFailed, ""
}

// connectPassword fetches the credentials once and connects through the
// normal SMTP/IMAP path, which seals them. The plaintext never leaves this call.
func (s *Service) connectPassword(ctx context.Context, orgID, userID uuid.UUID, row *models.StoreMailbox, name string) (*models.Email, string) {
	if s.Mailboxes == nil {
		return nil, connectFailed
	}
	creds, gerr := s.api().MailboxCredentials(ctx, row.GraphKitMailboxID)
	if gerr != nil {
		logUpstream("mailbox.credentials", gerr)
		return nil, connectFailed
	}
	secret, method, host := creds.Password, models.MailAuthPassword, mailhost.Microsoft365
	if row.Provider == graphkit.ProviderGoogle {
		// Google IMAP and SMTP accept only the app password.
		secret, method, host = creds.AppPassword, models.MailAuthAppPassword, mailhost.GoogleWorkspace
	}
	username := creds.Username
	if username == "" {
		username = row.Email
	}
	data := &models.NewSMTPIMAPAccount{Name: name, Email: row.Email, MailHost: string(host), AuthMethod: method, Purchased: true,
		SMTP: &models.Service{Username: username, Password: secret, Host: creds.SMTP.Host, Port: creds.SMTP.Port, Security: security(creds.SMTP.Security)},
		IMAP: &models.Service{Username: username, Password: secret, Host: creds.IMAP.Host, Port: creds.IMAP.Port, Security: security(creds.IMAP.Security)}}
	acc, xerr := s.Mailboxes.OnboardSMTPIMAP(ctx, userID.String(), &orgID, data)
	creds.Password, creds.AppPassword, secret = "", "", ""
	data.SMTP.Password, data.IMAP.Password = "", ""
	if xerr != nil {
		log.Warn().Str("store_mailbox_id", row.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: connecting a purchased mailbox with its password failed")
		return nil, connectFailed
	}
	return acc, ""
}

func security(s string) string {
	switch strings.ToLower(s) {
	case "ssl", "tls":
		return models.MailSecurityTLS
	case "starttls":
		return models.MailSecurityStartTLS
	}
	return ""
}

// purchaser is who the connected mailbox is recorded as connected by: the
// member who placed the order, else the workspace owner.
func (s *Service) purchaser(ctx context.Context, row *models.StoreMailbox) uuid.UUID {
	if row.OrderID != nil {
		if o, err := s.Repo.GetOrder(ctx, row.OrganizationID, *row.OrderID); err == nil && o != nil && o.CreatedBy != nil {
			return *o.CreatedBy
		}
	}
	if s.People != nil {
		if org, err := s.People.GetOrganization(ctx, row.OrganizationID); err == nil && org != nil {
			return org.OwnerUserID
		}
	}
	return uuid.Nil
}

// WakeMicrosoftConsent makes a workspace's mailboxes waiting on these domains' tenant consent connect on the next pass.
func (s *Service) WakeMicrosoftConsent(ctx context.Context, orgID uuid.UUID, domains []string) {
	rows, err := s.Repo.WakeConnectAction(ctx, orgID, models.ActionMicrosoftConsent, domains)
	if err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("store: waking connects after a tenant consent failed")
		return
	}
	for i := range rows {
		s.publishMailbox(ctx, &rows[i])
	}
}
