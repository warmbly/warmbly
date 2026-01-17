package role

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *roleService) Create(ctx context.Context, adminID uuid.UUID, data *models.CreateRole) (*models.Role, *errx.Error) {
	role, err := s.roleRepository.Create(ctx, data)
	if err != nil {
		return nil, err
	}

	s.delRoles(ctx)

	return role, nil
}

func (s *roleService) Get(ctx context.Context) ([]models.Role, *errx.Error) {
	roles, err := s.getRoles(ctx)
	if err != nil {
		return nil, err
	}
	if roles != nil {
		return roles, nil
	}

	roles, err = s.roleRepository.Get(ctx)
	if err != nil {
		return roles, err
	}

	s.delRoles(ctx)

	return roles, nil
}

func (s *roleService) Update(ctx context.Context, adminID, roleID uuid.UUID, data *models.UpdateRole) (*models.Role, *errx.Error) {
	role, err := s.roleRepository.Update(ctx, roleID, data)
	if err != nil {
		return nil, err
	}

	s.delRoles(ctx)

	return role, nil
}

func (s *roleService) Delete(ctx context.Context, adminID, roleID uuid.UUID) *errx.Error {
	if err := s.roleRepository.Delete(ctx, roleID); err != nil {
		return err
	}

	s.delRoles(ctx)

	return nil
}

func (s *roleService) Add(ctx context.Context, adminID, userID, roleID uuid.UUID) *errx.Error {
	return s.roleRepository.Add(ctx, userID, roleID)
}

func (s *roleService) Remove(ctx context.Context, adminID, userID, roleID uuid.UUID) *errx.Error {
	return s.roleRepository.Remove(ctx, userID, roleID)
}
