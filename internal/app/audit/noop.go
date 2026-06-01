package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// NoOpService is an AuditService that discards every call. It exists so
// handlers can call h.AuditService unconditionally without the embedded
// interface being nil — calling a method on a nil interface panics with a
// runtime nil-pointer deref, which the API would surface as a generic 500.
//
// The real persistence-backed service (NewService) is wired in
// cmd/backend/main.go; this remains as a safe fallback for tests or any
// entrypoint that has no database.
type NoOpService struct{}

func NewNoOpService() AuditService { return &NoOpService{} }

func (s *NoOpService) Log(_ context.Context, _ *models.CreateAuditLog) error {
	return nil
}

func (s *NoOpService) LogAction(
	_ context.Context,
	_, _ uuid.UUID,
	_ models.AuditAction,
	_ models.AuditEntityType,
	_ *uuid.UUID,
	_, _ string,
	_, _ map[string]string,
) {
}

func (s *NoOpService) Search(_ context.Context, _ *models.AuditLogSearch) (*models.AuditLogsResult, *errx.Error) {
	return &models.AuditLogsResult{Data: []models.AuditLog{}}, nil
}

func (s *NoOpService) Prune(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}
