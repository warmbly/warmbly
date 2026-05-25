package audit

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// NoOpService is an AuditService that discards every call. It exists
// so handlers can call h.AuditService.LogAction unconditionally without
// the embedded interface being nil — calling a method on a nil
// interface panics with a runtime nil-pointer deref, which the API was
// surfacing as a generic 500 (most visibly on /contacts/import/commit).
//
// Wire this in cmd/backend/main.go until a real persistence-backed
// AuditService is set up.
type NoOpService struct{}

func NewNoOpService() AuditService { return &NoOpService{} }

func (s *NoOpService) Log(_ context.Context, _ *models.CreateAuditLog) error {
	return nil
}

func (s *NoOpService) LogAction(
	_ context.Context,
	_ uuid.UUID,
	_ models.AuditAction,
	_ models.AuditEntityType,
	_ *uuid.UUID,
	_, _ string,
	_, _ map[string]string,
) {
}

func (s *NoOpService) GetUserLogs(_ context.Context, _ uuid.UUID, _ *models.AuditLogSearch) (*models.AuditLogsResult, *errx.Error) {
	return &models.AuditLogsResult{}, nil
}

func (s *NoOpService) GetEntityLogs(_ context.Context, _ models.AuditEntityType, _ uuid.UUID, _ int, _ string) (*models.AuditLogsResult, *errx.Error) {
	return &models.AuditLogsResult{}, nil
}
