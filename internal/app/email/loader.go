package email

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

// WireGraphDelta attaches the Graph delta-cursor repository so the reconciler can
// seed a Graph mailbox's saved per-folder cursors when (re)loading it. Optional;
// when unset, Graph mailboxes prime from empty on load.
func (s *emailService) WireGraphDelta(repo repository.EmailGraphDeltaRepository) {
	s.graphDelta = repo
}

// StartWorkerReconciler periodically ensures every active mailbox is assigned to
// a worker and loaded onto it. Workers hold accounts in memory only, so this is
// what makes onboarding, worker restarts, and reassignment converge: a fresh
// account gets picked up within one interval, and a restarted worker is
// re-seeded. PublishAddEmail is idempotent worker-side (already-loaded accounts
// are skipped), so re-publishing every tick is safe.
func (s *emailService) StartWorkerReconciler(ctx context.Context, interval time.Duration) {
	s.reconcileWorkerAccounts(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileWorkerAccounts(ctx)
		}
	}
}

func (s *emailService) reconcileWorkerAccounts(ctx context.Context) {
	ids, err := s.emailRepository.ListActiveWorkerAccounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("worker reconciler: list active accounts failed")
		return
	}
	for _, id := range ids {
		if err := s.LoadAccountOntoWorker(ctx, id); err != nil {
			log.Warn().Err(err).Str("email_id", id.String()).Msg("worker reconciler: load account failed")
		}
	}
}

// loadAccountBestEffort loads a freshly onboarded account onto its worker without
// blocking or failing the onboarding response; the reconciler is the safety net.
func (s *emailService) loadAccountBestEffort(ctx context.Context, accountID uuid.UUID) {
	if err := s.LoadAccountOntoWorker(ctx, accountID); err != nil {
		log.Warn().Err(err).Str("email_id", accountID.String()).Msg("initial account load onto worker failed")
	}
}

// LoadAccountOntoWorker assigns a worker if the account has none, rebuilds the
// account's decrypted credentials into an AddWorkerEmail payload, and publishes
// it so the worker loads the account into memory. Safe to call repeatedly.
func (s *emailService) LoadAccountOntoWorker(ctx context.Context, accountID uuid.UUID) error {
	acc, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return xerr
	}
	if acc == nil {
		return nil
	}

	workerID := acc.WorkerID
	if workerID == nil {
		// No worker yet: assign one now (OAuth onboarding never assigned).
		if acc.OrganizationID == nil || s.workerAssignment == nil {
			log.Warn().
				Str("email_id", acc.ID.String()).
				Bool("has_org", acc.OrganizationID != nil).
				Msg("cannot load mailbox onto a worker: missing organization or assignment service; account will not send or sync")
			return nil
		}
		assigned, err := s.workerAssignment.AssignWorkerToEmail(ctx, acc.ID, *acc.OrganizationID)
		if err != nil {
			return err
		}
		workerID = assigned
	}
	if workerID == nil {
		return nil
	}

	payload, err := s.buildAddWorkerEmail(ctx, acc)
	if err != nil {
		return err
	}
	if payload == nil {
		return nil
	}
	return s.publisher.PublishAddEmail(ctx, *workerID, payload)
}

// buildAddWorkerEmail reconstructs the worker payload for an account, decrypting
// its credentials and attaching the provider-specific data. Cfg is intentionally
// left zero: it is avro-excluded and the worker rebuilds it locally from its own
// oauth config.
func (s *emailService) buildAddWorkerEmail(ctx context.Context, acc *models.Email) (*models.AddWorkerEmail, error) {
	userID, err := uuid.Parse(acc.UserID)
	if err != nil {
		return nil, err
	}
	first, last := splitName(acc.Name)
	provider := models.InboxProvider(acc.Provider)

	out := &models.AddWorkerEmail{
		ID:        acc.ID,
		UserID:    userID,
		Email:     acc.Email,
		FirstName: first,
		LastName:  last,
		Type:      provider,
	}

	switch provider {
	case models.InboxProviderGoogle:
		creds, cerr := s.emailRepository.GetOAuthCredentials(ctx, acc.ID)
		if cerr != nil {
			return nil, cerr
		}
		var lastHistory uint64
		if acc.LastID != nil && *acc.LastID > 0 {
			lastHistory = uint64(*acc.LastID)
		}
		out.Google = &models.AddWorkerEmailGoogleData{
			Token:         oauthToken(creds),
			LastHistoryID: lastHistory,
		}
	case models.InboxProviderOutlook:
		creds, cerr := s.emailRepository.GetOAuthCredentials(ctx, acc.ID)
		if cerr != nil {
			return nil, cerr
		}
		out.Graph = &models.AddWorkerEmailGraphData{
			Token:      oauthToken(creds),
			DeltaLinks: s.deltaLinksFor(ctx, userID, acc.ID),
		}
	case models.InboxProviderSMTPIMAP:
		creds, cerr := s.emailRepository.GetSMTPCredentials(ctx, acc.ID)
		if cerr != nil {
			return nil, cerr
		}
		out.ImapSync = true
		out.SmtpImap = &models.AddWorkerEmailSmtpImapData{
			Credentials: &models.SmtpImap{
				SMTP: &models.Service{Host: creds.SMTPHost, Port: creds.SMTPPort, Username: creds.SMTPUser, Password: creds.SMTPPassword},
				IMAP: &models.Service{Host: creds.IMAPHost, Port: creds.IMAPPort, Username: creds.IMAPUser, Password: creds.IMAPPassword},
			},
		}
	default:
		return nil, nil
	}

	return out, nil
}

func (s *emailService) deltaLinksFor(ctx context.Context, userID, emailID uuid.UUID) map[string]string {
	if s.graphDelta == nil {
		return nil
	}
	links, err := s.graphDelta.Get(ctx, userID, emailID)
	if err != nil {
		return nil
	}
	return links
}

func oauthToken(c *repository.OAuthCredentials) *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		Expiry:       c.ExpiresAt,
		TokenType:    "Bearer",
	}
}

func splitName(name string) (firstName, lastName string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	parts := strings.SplitN(name, " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.TrimSpace(parts[1])
}
