package email

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
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

// WireEmailHistoryID attaches the Gmail history-cursor repository, the Google
// counterpart of WireGraphDelta. Optional; when unset, a reloaded Gmail mailbox
// falls back to the legacy email_accounts.last_id column.
func (s *emailService) WireEmailHistoryID(repo repository.EmailHistoryIDRepository) {
	s.historyID = repo
}

// reconcileRepublishInterval bounds how often the reconciler re-publishes a
// given account. The immediate onboarding load and any reassignment still fire
// right away (they call LoadAccountOntoWorker directly); this only throttles the
// steady-state safety-net loop so the fleet isn't re-shipping every account's
// decrypted credentials over Kafka every tick. A restarted worker is re-seeded
// within this window rather than within one tick.
const reconcileRepublishInterval = 5 * time.Minute

// StartWorkerReconciler periodically ensures every active mailbox is assigned to
// a worker and loaded onto it. Workers hold accounts in memory only, so this is
// what makes onboarding, worker restarts, and reassignment converge. Each
// account is republished at most once per reconcileRepublishInterval;
// PublishAddEmail is idempotent worker-side, so a republish is always safe.
func (s *emailService) StartWorkerReconciler(ctx context.Context, interval time.Duration) {
	lastPublished := map[uuid.UUID]time.Time{}
	s.reconcileWorkerAccounts(ctx, lastPublished)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileWorkerAccounts(ctx, lastPublished)
		}
	}
}

func (s *emailService) reconcileWorkerAccounts(ctx context.Context, lastPublished map[uuid.UUID]time.Time) {
	ids, err := s.emailRepository.ListActiveWorkerAccounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("worker reconciler: list active accounts failed")
		return
	}

	active := make(map[uuid.UUID]struct{}, len(ids))
	now := time.Now()
	for _, id := range ids {
		active[id] = struct{}{}
		if last, ok := lastPublished[id]; ok && now.Sub(last) < reconcileRepublishInterval {
			continue
		}
		if err := s.LoadAccountOntoWorker(ctx, id); err != nil {
			log.Warn().Err(err).Str("email_id", id.String()).Msg("worker reconciler: load account failed")
			continue
		}
		lastPublished[id] = now
	}

	// Drop throttle entries for accounts no longer active so the map can't grow
	// without bound as mailboxes are disconnected.
	for id := range lastPublished {
		if _, ok := active[id]; !ok {
			delete(lastPublished, id)
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

	workerID, rerr := s.releaseDeadWorker(ctx, acc.ID, acc.WorkerID)
	if rerr != nil {
		return rerr
	}

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

// releaseDeadWorker returns the worker a mailbox should load onto, releasing it
// first when the one it holds can no longer receive anything.
//
// A worker that has stopped heartbeating cannot be sent to, so a mailbox still
// pointing at one is stranded: every send fails with "email account not found
// in worker" and nothing re-places it, because assignment previously ran only
// when worker_id was NULL.
//
// That is routine rather than exotic. A worker started without WORKER_ID mints
// a fresh UUID on every boot, so each `docker compose up -d worker` leaves the
// previous row behind with its mailboxes still attached.
//
// Returning the current worker unchanged on a lookup failure is deliberate: a
// database blip must not churn placements, because moving a mailbox changes the
// IP it sends from.
func (s *emailService) releaseDeadWorker(ctx context.Context, accountID uuid.UUID, current *uuid.UUID) (*uuid.UUID, error) {
	if current == nil || s.workerAssignment == nil {
		return current, nil
	}

	live, err := s.workerAssignment.IsWorkerLive(ctx, *current)
	if err != nil {
		log.Warn().Err(err).Str("email_id", accountID.String()).
			Msg("could not check worker liveness; keeping the current assignment")
		return current, nil
	}
	if live {
		return current, nil
	}

	log.Info().
		Str("email_id", accountID.String()).
		Str("worker_id", current.String()).
		Msg("assigned worker is gone; releasing the mailbox so it can be placed on a live worker")
	if err := s.workerAssignment.UnassignWorkerFromEmail(ctx, accountID); err != nil {
		return nil, err
	}
	return nil, nil
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

	saveToSent := acc.SaveToSent
	out := &models.AddWorkerEmail{
		ID:             acc.ID,
		UserID:         userID,
		OrganizationID: acc.OrganizationID,
		Email:          acc.Email,
		FirstName:      first,
		LastName:       last,
		Type:           provider,
		Sync:           s.syncDataFor(ctx, acc.ID),
		// Only SMTP/IMAP acts on this; Gmail and Graph file their own copy.
		SaveToSent: &saveToSent,
	}

	switch provider {
	case models.InboxProviderGoogle:
		creds, cerr := s.emailRepository.GetOAuthCredentials(ctx, acc.ID)
		if cerr != nil {
			return nil, cerr
		}
		out.Google = &models.AddWorkerEmailGoogleData{
			Token:         oauthToken(creds),
			LastHistoryID: s.lastHistoryFor(ctx, userID, acc.ID, acc.LastID),
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
			Mailboxes: s.mailboxesFor(ctx, userID, acc.ID),
		}
	default:
		return nil, nil
	}

	return out, nil
}

// lastHistoryFor is the Gmail checkpoint a (re)loaded mailbox resumes from.
//
// The consumer writes every checkpoint to email_history_ids. This used to read
// email_accounts.last_id instead, which nothing writes, so the column is always
// NULL and every worker restart handed the mailbox a zero cursor. That silently
// re-bootstraps to Gmail's current historyId and skips everything that arrived
// since the last sync, unrecoverably: the history API only walks forward from
// the id it is given.
//
// legacyLastID stays as a fallback for rows carrying a value from before the
// checkpoint table existed.
func (s *emailService) lastHistoryFor(ctx context.Context, userID, emailID uuid.UUID, legacyLastID *int64) uint64 {
	if s.historyID != nil {
		if saved, err := s.historyID.Get(ctx, userID, emailID); err == nil && saved != nil && saved.HistoryID > 0 {
			return saved.HistoryID
		}
	}
	if legacyLastID != nil && *legacyLastID > 0 {
		return uint64(*legacyLastID)
	}
	return 0
}

// syncDataFor resolves the fair-use policy the mailbox syncs under and the
// state a previous worker left behind. Policy comes from instance settings
// (compiled defaults when none are wired), so an operator's change applies at
// the next load: onboarding, reassignment, or the reconciler's republish.
func (s *emailService) syncDataFor(ctx context.Context, emailID uuid.UUID) *models.AddWorkerEmailSyncData {
	budget := instancesettings.DefaultSync()
	if s.syncBudget != nil {
		budget = s.syncBudget.SyncBudget(ctx)
	}
	data := &models.AddWorkerEmailSyncData{
		Policy: models.SyncPolicy{
			BackfillDays:     budget.BackfillDays,
			BackfillMessages: budget.BackfillMessages,
			DailyMessages:    budget.DailyMessagesPerMailbox,
			OrgDailyMessages: budget.DailyMessagesPerOrg,
		},
	}
	if s.syncState != nil {
		if saved, err := s.syncState.Get(ctx, emailID); err == nil {
			data.State = saved
		} else {
			log.Warn().Err(err).Str("email_id", emailID.String()).Msg("sync state lookup failed; worker starts fresh")
		}
	}
	return data
}

// mailboxesFor is the IMAP folder state (name, UIDVALIDITY, HIGHESTMODSEQ)
// the consumer saved from UPDATE_MAILBOX events. Nil when nothing is saved
// or the repository is not wired, which the worker treats as a first sight.
func (s *emailService) mailboxesFor(ctx context.Context, userID, emailID uuid.UUID) []models.Mailbox {
	if s.mailboxes == nil {
		return nil
	}
	saved, err := s.mailboxes.ListMailboxes(ctx, userID, emailID)
	if err != nil {
		log.Warn().Err(err).Str("email_id", emailID.String()).Msg("mailbox folder state lookup failed; worker re-baselines")
		return nil
	}
	return saved
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
