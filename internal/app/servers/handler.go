package servers

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *serversService) GetWorkers(ctx context.Context, userID uuid.UUID) ([]models.Worker, *errx.Error) {
	user, err := s.userService.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if err := s.roleService.HavePermission(ctx, user.Roles, []uint8{models.PermManageServers}); err != nil {
		return nil, err
	}

	workers, err := s.getWorkers(ctx)
	if err != nil {
		return nil, err
	}

	workers, err = s.serversRepository.GetWorkers(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.saveWorkers(ctx, workers); err != nil {
		return nil, err
	}

	return workers, nil
}
