package audit

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type AuditService interface {
	// Log writes a single audit entry synchronously.
	Log(ctx context.Context, log *models.CreateAuditLog) error

	// LogAction records an action without blocking the caller. orgID scopes the
	// entry to an organization and actorID identifies the member who performed
	// it. Sensitive secret values must never be passed in changes/metadata.
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)

	// Search returns an organization's audit trail. params.OrgID must be set by
	// the caller from the session; it is the only tenancy boundary.
	Search(ctx context.Context, params *models.AuditLogSearch) (*models.AuditLogsResult, *errx.Error)

	// Prune removes entries older than the retention window. Returns the number
	// of rows deleted.
	Prune(ctx context.Context, retention time.Duration) (int64, error)
}

type auditService struct {
	repo      repository.AuditRepository
	publisher *pubsub.StreamingPublisher
}

// NewService builds the audit service. publisher may be nil (e.g. when Pub/Sub
// is not configured); realtime emission is then skipped.
func NewService(repo repository.AuditRepository, publisher *pubsub.StreamingPublisher) AuditService {
	return &auditService{
		repo:      repo,
		publisher: publisher,
	}
}

func (s *auditService) Log(ctx context.Context, log *models.CreateAuditLog) error {
	return s.repo.Log(ctx, log)
}

func (s *auditService) LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string) {
	// Never block the request, and never let a logging failure (or a panic in
	// the write path) bubble up and take down the handler. Audit logging is
	// best-effort: the trail is valuable but it must not become a liability on
	// the hot path.
	if orgID == uuid.Nil {
		return
	}

	log := &models.CreateAuditLog{
		OrgID:      orgID,
		UserID:     actorID,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		IPAddress:  ipAddress,
		UserAgent:  userAgent,
		Changes:    changes,
		Metadata:   metadata,
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				sentry.CurrentHub().Recover(r)
			}
		}()
		if err := s.repo.Log(context.Background(), log); err != nil {
			sentry.CaptureException(err)
			return
		}
		// Notify the org's dashboard to refetch the activity log live. Carries
		// no sensitive detail — just the org/actor/action/entity.
		if s.publisher != nil {
			s.publisher.PublishAuditCreated(context.Background(), orgID, actorID, string(action), string(entityType), entityID)
		}
	}()
}

func (s *auditService) Search(ctx context.Context, params *models.AuditLogSearch) (*models.AuditLogsResult, *errx.Error) {
	if params == nil || params.OrgID == nil {
		return nil, errx.ErrAuth
	}

	if params.Limit <= 0 || params.Limit > 200 {
		params.Limit = 50
	}

	result, err := s.repo.Search(ctx, params)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return result, nil
}

func (s *auditService) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)
	return s.repo.PruneOlderThan(ctx, cutoff)
}

// Helper functions for common audit log scenarios. Each takes orgID so the
// entry is scoped to the organization the action happened in.

// LogCreate logs a create action.
func LogCreate(s AuditService, ctx context.Context, orgID, actorID uuid.UUID, entityType models.AuditEntityType, entityID uuid.UUID, ipAddress, userAgent string, metadata map[string]string) {
	s.LogAction(ctx, orgID, actorID, models.AuditActionCreate, entityType, &entityID, ipAddress, userAgent, nil, metadata)
}

// LogUpdate logs an update action with changes.
func LogUpdate(s AuditService, ctx context.Context, orgID, actorID uuid.UUID, entityType models.AuditEntityType, entityID uuid.UUID, ipAddress, userAgent string, changes map[string]string) {
	s.LogAction(ctx, orgID, actorID, models.AuditActionUpdate, entityType, &entityID, ipAddress, userAgent, changes, nil)
}

// LogDelete logs a delete action.
func LogDelete(s AuditService, ctx context.Context, orgID, actorID uuid.UUID, entityType models.AuditEntityType, entityID uuid.UUID, ipAddress, userAgent string) {
	s.LogAction(ctx, orgID, actorID, models.AuditActionDelete, entityType, &entityID, ipAddress, userAgent, nil, nil)
}
