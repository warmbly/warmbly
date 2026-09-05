package jobs

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// OrgNotifier raises a permission-targeted org notification (in-app feed +
// each member's enabled channels, email digest-coalesced). Satisfied by
// *notification.Service; local interface to avoid an import cycle.
type OrgNotifier interface {
	NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)
}

// OperatorNotifier is the instance-wide operator alert surface, declared here
// so this package needs no import of it. Nil disables it.
type OperatorNotifier interface {
	NotifyOperator(key, title, summary string, fields map[string]string)
}

// notifyOperatorWorkerDown alerts the operator that a worker is gone. It shares
// the same once-per-incident SetNX guard shape as the tenant notice, under its
// own key so the two audiences are independent.
func (s *JobsService) notifyOperatorWorkerDown(ctx context.Context, workerID uuid.UUID, mailboxes int, reassigned bool) {
	if s.OpsNotifier == nil || s.Cache == nil {
		return
	}
	ok, err := s.Cache.SetNX(ctx, "worker:opsnotify:"+workerID.String(), "1", 6*time.Hour).Result()
	if err != nil || !ok {
		return
	}
	outcome := "Mailboxes were moved to a healthy worker automatically."
	if !reassigned {
		outcome = "No healthy replacement of the same tier was available, so sending from those mailboxes is paused."
	}
	s.OpsNotifier.NotifyOperator(
		"worker.offline",
		"Worker stopped responding",
		outcome,
		map[string]string{
			"Worker":     workerID.String(),
			"Mailboxes":  strconv.Itoa(mailboxes),
			"Reassigned": map[bool]string{true: "yes", false: "no"}[reassigned],
		},
	)
}

// notifyWorkerDown tells each affected org's manage_emails members about a
// dead worker, at most once per worker incident: detection reruns every
// interval while the worker stays down, and the SetNX guard keeps that from
// re-alerting. The shared group key coalesces an org's recipients into one
// email with everyone in To.
func (s *JobsService) notifyWorkerDown(ctx context.Context, workerID uuid.UUID, orgs map[uuid.UUID]int, reassigned bool) {
	if s.Notifier == nil || len(orgs) == 0 {
		return
	}
	ok, err := s.Cache.SetNX(ctx, "worker:downnotify:"+workerID.String(), "1", 6*time.Hour).Result()
	if err != nil || !ok {
		return
	}
	for orgID, n := range orgs {
		noun := fmt.Sprintf("%d of your mailboxes were", n)
		if n == 1 {
			noun = "One of your mailboxes was"
		}
		body := noun + " on a sending worker that stopped responding. "
		if reassigned {
			body += "They were moved to a healthy worker automatically; no action is needed."
			if n == 1 {
				body = noun + " on a sending worker that stopped responding. It was moved to a healthy worker automatically; no action is needed."
			}
		} else {
			body += "Sending from them is paused until a replacement worker is available."
		}
		s.Notifier.NotifyOrg(ctx, orgID, models.PermManageEmails, uuid.Nil, models.NotifWorkerDowntime,
			"Sending worker went offline", body, "/app/emails", nil,
			"worker_down:"+workerID.String())
	}
}

// StartDeadWorkerDetection periodically checks for workers whose heartbeat has
// expired and reassigns their email accounts to healthy workers.
// Runs every interval until the context is cancelled.
func (s *JobsService) StartDeadWorkerDetection(ctx context.Context, interval time.Duration) {
	if s.WorkerRepo == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			detectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			s.detectDeadWorkers(detectCtx)
			cancel()
		}
	}
}

func (s *JobsService) detectDeadWorkers(ctx context.Context) {
	// Get all workers from the database
	workers, err := s.WorkerRepo.GetAllActiveWorkers(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("dead worker detection: failed to list workers")
		return
	}

	for _, w := range workers {
		key := fmt.Sprintf("worker:heartbeat:%s", w.ID.String())
		exists, err := s.Cache.Exists(ctx, key).Result()
		if err != nil {
			continue
		}

		if exists > 0 {
			continue // Worker is alive
		}

		// Worker heartbeat expired - mark as stale and reassign emails
		log.Warn().Str("worker_id", w.ID.String()).Msg("dead worker detected - heartbeat expired")

		// Get all email accounts assigned to this worker
		accountIDs, err := s.WorkerRepo.GetEmailAccountsByWorkerID(ctx, w.ID)
		if err != nil {
			log.Error().Err(err).Str("worker_id", w.ID.String()).Msg("failed to get accounts for dead worker")
			continue
		}

		if len(accountIDs) == 0 {
			// Nothing to move: retire the row so it stops being rescanned
			// every interval. Churned ids without WORKER_ID accumulate here
			// forever otherwise; a returning worker reactivates itself on its
			// next heartbeat upsert.
			s.deactivateIfLongDead(ctx, w)
			continue
		}

		// Find a healthy replacement worker of the same tier
		replacement, err := s.findHealthyWorker(ctx, w)
		if err != nil || replacement == nil {
			log.Warn().Str("worker_id", w.ID.String()).Msg("no healthy replacement worker found")
			s.notifyWorkerDown(ctx, w.ID, s.accountOrgs(ctx, accountIDs), false)
			s.notifyOperatorWorkerDown(ctx, w.ID, len(accountIDs), false)
			continue
		}

		// Reassign accounts to the healthy worker
		reassigned := 0
		affectedOrgs := map[uuid.UUID]int{}
		for _, accountID := range accountIDs {
			if err := s.WorkerRepo.UpdateEmailAccountWorker(ctx, accountID, replacement.ID); err != nil {
				log.Error().Err(err).Str("account_id", accountID.String()).Msg("failed to reassign email account")
				continue
			}
			reassigned++

			// Keep the placement counters honest on both rows; without this
			// every auto-reassignment permanently skews account_count. The
			// move itself already succeeded, so a counter failure is logged
			// rather than retried: capacity self-corrects on the next
			// placement pass, and unwinding the move would strand the mailbox.
			if cerr := s.WorkerRepo.DecrementAccountCount(ctx, w.ID); cerr != nil {
				log.Warn().Err(cerr).Str("worker_id", w.ID.String()).Msg("dead worker reassign: account_count not decremented")
			}
			if cerr := s.WorkerRepo.IncrementAccountCount(ctx, replacement.ID); cerr != nil {
				log.Warn().Err(cerr).Str("worker_id", replacement.ID.String()).Msg("dead worker reassign: account_count not incremented")
			}

			account, aerr := s.EmailRepository.GetByID(ctx, accountID)
			if aerr != nil || account == nil {
				continue
			}
			if account.OrganizationID != nil {
				affectedOrgs[*account.OrganizationID]++
			}

			// The backend's worker reconciler loads the account onto its new
			// worker with the full payload (decrypted credentials, cursors,
			// sync policy). Publishing a bare ADD_EMAIL from here only made
			// the worker reject it and log an error per mailbox.
		}

		if reassigned > 0 {
			log.Info().
				Str("dead_worker", w.ID.String()).
				Str("replacement", replacement.ID.String()).
				Int("reassigned", reassigned).
				Msg("email accounts reassigned from dead worker")

			// Record in admin_audit_log so the dashboard's audit viewer
			// shows when and where the fleet auto-reassigned. uuid.Nil for
			// admin_user_id signals "system action".
			if s.AdminRepo != nil {
				_ = s.AdminRepo.CreateAuditLog(ctx, &models.AdminAuditLog{
					ID:          uuid.New(),
					AdminUserID: uuid.Nil,
					Action:      "auto_reassign",
					TargetType:  "worker",
					TargetID:    w.ID,
					Details: map[string]any{
						"replacement":         replacement.ID.String(),
						"accounts_reassigned": reassigned,
						"reason":              "heartbeat_expired",
					},
					IPAddress: "",
					UserAgent: "system",
					CreatedAt: time.Now(),
				})
			}

			s.notifyWorkerDown(ctx, w.ID, affectedOrgs, true)
			s.notifyOperatorWorkerDown(ctx, w.ID, reassigned, true)
		}

		if reassigned == len(accountIDs) {
			s.deactivateIfLongDead(ctx, w)
		}
	}
}

// deactivateIfLongDead retires a heartbeat-expired worker row, but only when
// its registry timestamp is stale too. During a Redis outage every heartbeat
// key vanishes at once while POSTed beats keep last_seen_at fresh; gating on
// both signals keeps that from deactivating the whole live fleet.
//
// The worker is re-read first because the caller's copy is a snapshot from the
// top of the scan, which walks the whole fleet and can take a while: a worker
// that booted during the scan must not be deactivated on stale evidence.
func (s *JobsService) deactivateIfLongDead(ctx context.Context, w models.Worker) {
	current, err := s.WorkerRepo.GetByID(ctx, w.ID)
	if err != nil || current == nil {
		return
	}
	// Never seen at all means the age is unknown, not old. Leave it alone
	// rather than retiring a row that may be mid-registration.
	if current.LastSeenAt == nil || time.Since(*current.LastSeenAt) < 10*time.Minute {
		return
	}
	// One last heartbeat check against the freshly-read row.
	if n, herr := s.Cache.Exists(ctx, "worker:heartbeat:"+w.ID.String()).Result(); herr != nil || n > 0 {
		return
	}
	if err := s.WorkerRepo.DeactivateWorker(ctx, w.ID); err != nil {
		log.Warn().Err(err).Str("worker_id", w.ID.String()).Msg("failed to deactivate dead worker")
		return
	}
	log.Info().Str("worker_id", w.ID.String()).Msg("dead worker deactivated")
}

// accountOrgs resolves which orgs own the given accounts (org -> count).
func (s *JobsService) accountOrgs(ctx context.Context, accountIDs []uuid.UUID) map[uuid.UUID]int {
	orgs := map[uuid.UUID]int{}
	for _, id := range accountIDs {
		if account, err := s.EmailRepository.GetByID(ctx, id); err == nil && account != nil && account.OrganizationID != nil {
			orgs[*account.OrganizationID]++
		}
	}
	return orgs
}

func (s *JobsService) findHealthyWorker(ctx context.Context, deadWorker models.Worker) (*models.Worker, error) {
	// Get workers of the same tier that are alive
	freeTier := deadWorker.FreeTier
	workers, err := s.WorkerRepo.GetSharedWorkersByTier(ctx, freeTier)
	if err != nil {
		return nil, err
	}

	for _, w := range workers {
		if w.ID == deadWorker.ID {
			continue
		}
		// Check if this worker is alive
		key := fmt.Sprintf("worker:heartbeat:%s", w.ID.String())
		exists, err := s.Cache.Exists(ctx, key).Result()
		if err != nil || exists == 0 {
			continue
		}
		return &w, nil
	}

	return nil, nil
}
