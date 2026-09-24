package email

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
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

// reconcileRepublishInterval is how often the safety net re-ships a mailbox
// that nothing else changed. A new placement, a move and a dead worker are
// acted on within one tick; onboarding and a worker's boot reload ship at once.
// Each mailbox's turn is spread across the interval, because re-shipping the
// whole fleet in one tick queued hundreds of commands on every worker at once.
const reconcileRepublishInterval = 30 * time.Minute

// reconcileEntry is what the reconciler remembers about one mailbox.
type reconcileEntry struct {
	worker uuid.UUID
	next   time.Time
}

// StartWorkerReconciler periodically ensures every active mailbox is assigned to
// a worker and loaded onto it. Workers hold accounts in memory only, so this is
// what makes onboarding, worker restarts, and reassignment converge.
// PublishAddEmail is idempotent worker-side, so a republish is always safe.
func (s *emailService) StartWorkerReconciler(ctx context.Context, interval time.Duration) {
	seen := map[uuid.UUID]reconcileEntry{}
	s.reconcileWorkerAccounts(ctx, seen)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileWorkerAccounts(ctx, seen)
		}
	}
}

// spreadTurn is a random point in the second half of the interval, so turns never bunch up again.
func spreadTurn(now time.Time) time.Time {
	half := int64(reconcileRepublishInterval / 2)
	return now.Add(time.Duration(half + rand.Int63n(half)))
}

func (s *emailService) reconcileWorkerAccounts(ctx context.Context, seen map[uuid.UUID]reconcileEntry) {
	rows, err := s.emailRepository.ListActiveWorkerAccounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("worker reconciler: list active accounts failed")
		return
	}
	live := map[uuid.UUID]bool{}
	isLive := func(id uuid.UUID) bool {
		v, ok := live[id]
		if !ok {
			v = true
			if s.workerAssignment != nil {
				if ok, err := s.workerAssignment.IsWorkerLive(ctx, id); err == nil {
					v = ok
				}
			}
			live[id] = v
		}
		return v
	}
	now := time.Now()
	for _, r := range reconcileDue(rows, seen, now, isLive) {
		if err := s.LoadAccountOntoWorker(ctx, r.ID); err != nil {
			log.Warn().Err(err).Str("email_id", r.ID.String()).Msg("worker reconciler: load account failed")
			continue
		}
		var worker uuid.UUID
		if r.WorkerID != nil {
			worker = *r.WorkerID
		}
		seen[r.ID] = reconcileEntry{worker: worker, next: spreadTurn(now)}
	}
}

// reconcileDue picks the mailboxes to ship this tick: at once when unplaced,
// on a dead worker or moved, otherwise when their spread-out turn comes. It
// also schedules first sightings and forgets mailboxes that are gone.
func reconcileDue(rows []repository.MailboxAssignment, seen map[uuid.UUID]reconcileEntry, now time.Time, isLive func(uuid.UUID) bool) []repository.MailboxAssignment {
	var due []repository.MailboxAssignment
	active := make(map[uuid.UUID]struct{}, len(rows))
	for _, r := range rows {
		active[r.ID] = struct{}{}
		entry, known := seen[r.ID]
		urgent := r.WorkerID == nil || !isLive(*r.WorkerID) || (known && entry.worker != *r.WorkerID)
		switch {
		case urgent:
		case !known:
			// First sight since boot: onboarding or the worker's boot reload
			// already shipped it, so its safety-net turn is spread out.
			seen[r.ID] = reconcileEntry{worker: *r.WorkerID, next: now.Add(time.Duration(rand.Int63n(int64(reconcileRepublishInterval))))}
			continue
		case now.Before(entry.next):
			continue
		}
		due = append(due, r)
	}
	for id := range seen {
		if _, ok := active[id]; !ok {
			delete(seen, id)
		}
	}
	return due
}

// ReloadWorkerAccounts publishes every active mailbox assigned to workerID
// back onto it. Called from the boot heartbeat, so a restarted worker is
// sending and syncing again within seconds rather than after the reconciler's
// next republish window.
func (s *emailService) ReloadWorkerAccounts(ctx context.Context, workerID uuid.UUID) {
	ids, err := s.emailRepository.ListActiveAccountsByWorker(ctx, workerID)
	if err != nil {
		log.Warn().Err(err).Str("worker_id", workerID.String()).Msg("worker boot reload: list accounts failed")
		return
	}
	loaded := 0
	for _, id := range ids {
		if err := s.LoadAccountOntoWorker(ctx, id); err != nil {
			log.Warn().Err(err).Str("email_id", id.String()).Str("worker_id", workerID.String()).Msg("worker boot reload: load account failed")
			continue
		}
		loaded++
	}
	if len(ids) > 0 {
		log.Info().Str("worker_id", workerID.String()).Int("loaded", loaded).Int("assigned", len(ids)).Msg("worker boot reload: mailboxes re-shipped")
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
// it so the worker loads the account into memory. Safe to call repeatedly, and
// a no-op for a mailbox that is not active.
func (s *emailService) LoadAccountOntoWorker(ctx context.Context, accountID uuid.UUID) error {
	acc, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return xerr
	}
	if acc == nil {
		return nil
	}
	// A mailbox that is not active must never be shipped to a worker. The
	// reconciler reads the active list a tick before it publishes, so without
	// this it can put back a mailbox that was deactivated in between and undo
	// the removal the consumer just sent.
	if acc.Status != "active" {
		return nil
	}
	// A delegated mailbox without its grant (it arrived in an archive, or the grant
	// went) has nothing to sign in with; it waits inactive until the domain is
	// connected again, which relinks and reactivates it.
	if acc.AuthMethod == models.MailAuthDelegated && acc.DomainGrantID == nil {
		if xerr := s.emailRepository.SetStatus(ctx, acc.ID, "inactive"); xerr != nil {
			return xerr
		}
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

// dropFromWorker tells the worker holding a mailbox to drop it from memory,
// the same removal the consumer sends when a provider error deactivates one.
// Workers hold accounts in memory and only filter on status at startup, so
// without this a disabled or disconnected mailbox syncs until that worker
// restarts. A send already dispatched is answered with EMAIL_FAILED, which
// walks its reservation back.
//
// The assignment is read on its own because the row an update returns carries
// no worker_id, which is what made the consumer's removal unreachable in #218.
func (s *emailService) dropFromWorker(ctx context.Context, userID string, accountID uuid.UUID) *errx.Error {
	if s.publisher == nil {
		return nil
	}

	workerID, xerr := s.emailRepository.GetWorkerID(ctx, accountID)
	if xerr != nil {
		return xerr
	}
	if workerID == nil {
		return nil
	}

	if err := s.publisher.PublishRemoveEmail(ctx, *workerID, &models.RemoveWorkerEmail{
		UserID:  userID,
		EmailID: accountID.String(),
	}); err != nil {
		log.Warn().Err(err).
			Str("email_id", accountID.String()).
			Str("worker_id", workerID.String()).
			Msg("could not tell the worker to drop the mailbox")
		return errx.ErrEmailWorkerUnreachable
	}
	return nil
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
	sync, err := s.syncDataFor(ctx, acc.ID)
	if err != nil {
		return nil, err
	}
	out := &models.AddWorkerEmail{
		ID:             acc.ID,
		UserID:         userID,
		OrganizationID: acc.OrganizationID,
		Email:          acc.Email,
		FirstName:      first,
		LastName:       last,
		Type:           provider,
		Sync:           sync,
		// Only SMTP/IMAP acts on this; Gmail and Graph file their own copy.
		SaveToSent: &saveToSent,
	}

	// A mailbox under an administrator's grant has no stored credential
	// either; the worker draws tokens the control plane mints per use.
	if acc.AuthMethod == models.MailAuthDelegated {
		d, xerr := s.emailRepository.GetDelegation(ctx, acc.ID)
		if xerr != nil {
			return nil, xerr
		}
		if d == nil {
			return nil, nil
		}
		out.Brokered = true
		switch provider {
		case models.InboxProviderGoogle:
			out.Google = &models.AddWorkerEmailGoogleData{LastHistoryID: s.lastHistoryFor(ctx, userID, acc.ID, acc.LastID)}
		case models.InboxProviderOutlook:
			out.Graph = &models.AddWorkerEmailGraphData{DeltaLinks: s.deltaLinksFor(ctx, userID, acc.ID), User: d.Subject}
		default:
			return nil, nil
		}
		return out, nil
	}

	// A managed mailbox has no local credential; the worker draws brokered tokens.
	if s.cloudLink != nil {
		if m, err := s.cloudLink.GetByAccount(ctx, acc.ID); err == nil && m != nil && m.Managed {
			out.Brokered = true
			switch provider {
			case models.InboxProviderGoogle:
				out.Google = &models.AddWorkerEmailGoogleData{LastHistoryID: s.lastHistoryFor(ctx, userID, acc.ID, acc.LastID)}
			case models.InboxProviderOutlook:
				out.Graph = &models.AddWorkerEmailGraphData{DeltaLinks: s.deltaLinksFor(ctx, userID, acc.ID)}
			default:
				return nil, nil
			}
			return out, nil
		}
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
				SMTP: &models.Service{Host: creds.SMTPHost, Port: creds.SMTPPort, Username: creds.SMTPUser, Password: creds.SMTPPassword, Security: creds.SMTPSecurity},
				IMAP: &models.Service{Host: creds.IMAPHost, Port: creds.IMAPPort, Username: creds.IMAPUser, Password: creds.IMAPPassword, Security: creds.IMAPSecurity},
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
func (s *emailService) syncDataFor(ctx context.Context, emailID uuid.UUID) (*models.AddWorkerEmailSyncData, error) {
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
	// The skip list is part of the policy a republish replaces on the loaded
	// mailbox, so a failed read cannot fall back to "skip nothing": that
	// would have the worker baseline and import the excluded folders until
	// the next republish. The load fails instead and the reconciler retries.
	skip, xerr := s.emailRepository.GetSyncSkipFolders(ctx, emailID)
	if xerr != nil {
		return nil, fmt.Errorf("sync skip folders lookup: %w", xerr)
	}
	data.Policy.SkipFolders = skip
	// A pool-linked mailbox is a warmup-only mirror: no history import.
	if s.poolLink != nil {
		if linked, err := s.poolLink.GetMailboxByAccount(ctx, emailID); err == nil && linked != nil {
			data.Policy.BackfillDays = 1
			data.Policy.BackfillMessages = 25
		}
	}
	if s.syncState != nil {
		if saved, err := s.syncState.Get(ctx, emailID); err == nil {
			data.State = saved
		} else {
			log.Warn().Err(err).Str("email_id", emailID.String()).Msg("sync state lookup failed; worker starts fresh")
		}
	}
	return data, nil
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
