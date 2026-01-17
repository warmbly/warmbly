package role

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *roleService) HavePermission(ctx context.Context, roles []uuid.UUID, permissions []uint8) *errx.Error {
	rs, err := s.Get(ctx)
	if err != nil {
		return err
	}

	var totalPerms uint8 = 0

	for r := range roles {
		if role := findRole(rs, roles[r]); role != nil {
			totalPerms |= role.Permissions
		}
	}

	for _, perm := range permissions {
		if totalPerms&perm == 0 {
			return errx.ErrForbidden
		}
	}

	return nil
}

func findRole(roles []models.Role, id uuid.UUID) *models.Role {
	for r := range roles {
		if roles[r].ID == id {
			return &roles[r]
		}
	}

	return nil
}
