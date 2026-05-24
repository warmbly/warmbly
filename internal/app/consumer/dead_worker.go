package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

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
			continue
		}

		// Find a healthy replacement worker of the same tier
		replacement, err := s.findHealthyWorker(ctx, w)
		if err != nil || replacement == nil {
			log.Warn().Str("worker_id", w.ID.String()).Msg("no healthy replacement worker found")
			continue
		}

		// Reassign accounts to the healthy worker
		reassigned := 0
		for _, accountID := range accountIDs {
			if err := s.WorkerRepo.UpdateEmailAccountWorker(ctx, accountID, replacement.ID); err != nil {
				log.Error().Err(err).Str("account_id", accountID.String()).Msg("failed to reassign email account")
				continue
			}
			reassigned++

			// Publish AddEmail event to the new worker so it picks up the account
			if s.Publisher != nil {
				account, aerr := s.EmailRepository.GetByID(ctx, accountID)
				if aerr == nil && account != nil {
					userUUID, _ := uuid.Parse(account.UserID)
					_ = s.Publisher.PublishAddEmail(ctx, replacement.ID, &models.AddWorkerEmail{
						ID:       account.ID,
						UserID:   userUUID,
						Email:    account.Email,
						Type:     models.InboxProvider(account.Provider),
						ImapSync: true,
					})
				}
			}
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
		}
	}
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
