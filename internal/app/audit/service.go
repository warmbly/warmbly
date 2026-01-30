package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type AuditService interface {
	// Log a new audit entry
	Log(ctx context.Context, log *models.CreateAuditLog) error

	// Log from gin context (extracts IP, user agent)
	LogAction(ctx context.Context, userID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)

	// Query logs
	GetUserLogs(ctx context.Context, userID uuid.UUID, params *models.AuditLogSearch) (*models.AuditLogsResult, *errx.Error)
	GetEntityLogs(ctx context.Context, entityType models.AuditEntityType, entityID uuid.UUID, limit int, cursor string) (*models.AuditLogsResult, *errx.Error)
}

type auditService struct {
	repo repository.AuditRepository
}

func NewService(repo repository.AuditRepository) AuditService {
	return &auditService{
		repo: repo,
	}
}

func (s *auditService) Log(ctx context.Context, log *models.CreateAuditLog) error {
	return s.repo.Log(ctx, log)
}

func (s *auditService) LogAction(ctx context.Context, userID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string) {
	// Fire and forget - don't block the request
	go func() {
		log := &models.CreateAuditLog{
			UserID:     userID,
			Action:     action,
			EntityType: entityType,
			EntityID:   entityID,
			IPAddress:  ipAddress,
			UserAgent:  userAgent,
			Changes:    changes,
			Metadata:   metadata,
		}
		_ = s.repo.Log(context.Background(), log)
	}()
}

func (s *auditService) GetUserLogs(ctx context.Context, userID uuid.UUID, params *models.AuditLogSearch) (*models.AuditLogsResult, *errx.Error) {
	if params == nil {
		params = &models.AuditLogSearch{}
	}
	params.UserID = &userID

	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 50
	}

	// Default to today if no date range
	if params.Since == nil {
		today := time.Now().Truncate(24 * time.Hour)
		params.Since = &today
	}

	result, err := s.repo.Search(ctx, params)
	if err != nil {
		return nil, errx.InternalError()
	}

	return result, nil
}

func (s *auditService) GetEntityLogs(ctx context.Context, entityType models.AuditEntityType, entityID uuid.UUID, limit int, cursor string) (*models.AuditLogsResult, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	result, err := s.repo.GetByEntity(ctx, string(entityType), entityID, limit, cursor)
	if err != nil {
		return nil, errx.InternalError()
	}

	return result, nil
}

// Helper functions for common audit log scenarios

// LogCreate logs a create action
func LogCreate(s AuditService, ctx context.Context, userID uuid.UUID, entityType models.AuditEntityType, entityID uuid.UUID, ipAddress, userAgent string, metadata map[string]string) {
	s.LogAction(ctx, userID, models.AuditActionCreate, entityType, &entityID, ipAddress, userAgent, nil, metadata)
}

// LogUpdate logs an update action with changes
func LogUpdate(s AuditService, ctx context.Context, userID uuid.UUID, entityType models.AuditEntityType, entityID uuid.UUID, ipAddress, userAgent string, changes map[string]string) {
	s.LogAction(ctx, userID, models.AuditActionUpdate, entityType, &entityID, ipAddress, userAgent, changes, nil)
}

// LogDelete logs a delete action
func LogDelete(s AuditService, ctx context.Context, userID uuid.UUID, entityType models.AuditEntityType, entityID uuid.UUID, ipAddress, userAgent string) {
	s.LogAction(ctx, userID, models.AuditActionDelete, entityType, &entityID, ipAddress, userAgent, nil, nil)
}

// LogLogin logs a login action
func LogLogin(s AuditService, ctx context.Context, userID uuid.UUID, ipAddress, userAgent string, metadata map[string]string) {
	s.LogAction(ctx, userID, models.AuditActionLogin, models.AuditEntitySession, nil, ipAddress, userAgent, nil, metadata)
}

// LogLogout logs a logout action
func LogLogout(s AuditService, ctx context.Context, userID uuid.UUID, ipAddress, userAgent string) {
	s.LogAction(ctx, userID, models.AuditActionLogout, models.AuditEntitySession, nil, ipAddress, userAgent, nil, nil)
}
