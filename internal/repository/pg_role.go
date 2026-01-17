package repository

import (
	"context"
	"fmt"
	"math/bits"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

type RoleRepository interface {
	Create(ctx context.Context, data *models.CreateRole) (*models.Role, *errx.Error)
	Get(ctx context.Context) ([]models.Role, *errx.Error)
	Update(ctx context.Context, roleID uuid.UUID, data *models.UpdateRole) (*models.Role, *errx.Error)
	Delete(ctx context.Context, roleID uuid.UUID) *errx.Error

	Add(ctx context.Context, userID, roleID uuid.UUID) *errx.Error
	Remove(ctx context.Context, userID, roleID uuid.UUID) *errx.Error
}

type roleRepository struct {
	DB      *db.DB
	Encrypt *encrypt.Encrypter
}

func NewRoleRepostory(db *db.DB) RoleRepository {
	return &roleRepository{
		DB: db,
	}
}

const RoleSelect = `id, permissions, name, color, created_at, updated_at`

func GetRole(rows db.Scannable, r *models.Role) error {
	return rows.Scan(&r.ID, &r.Permissions, &r.Name, &r.Color, &r.CreatedAt, &r.UpdatedAt)
}

func (r *roleRepository) Get(ctx context.Context) ([]models.Role, *errx.Error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM roles
	`, RoleSelect)

	rows, err := r.DB.Query(
		ctx,
		query,
	)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, errx.InternalError()
	}

	var roles []models.Role

	for rows.Next() {
		var r models.Role
		err := GetRole(rows, &r)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
	}

	sort.Slice(roles, func(i, j int) bool {
		return bits.OnesCount8(roles[i].Permissions) > bits.OnesCount8(roles[j].Permissions)
	})

	return roles, nil
}

func (r *roleRepository) Create(ctx context.Context, data *models.CreateRole) (*models.Role, *errx.Error) {
	if !validate.RoleName(data.Name) {
		return nil, errx.ErrRoleName
	}
	if !crypt.IsValidHexColor(data.Color) {
		return nil, errx.ErrColor
	}

	query := fmt.Sprintf(`
			INSERT INTO roles (
			 id, name, color
			) VALUES (
			 gen_random_uuid(), $1, $2
			) RETURNING %s
			`, RoleSelect)

	params := []any{
		data.Name,
		data.Color,
	}

	row := r.DB.QueryRow(
		ctx,
		query,
		params...,
	)

	var rol models.Role
	if err := GetRole(row, &rol); err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &rol, nil
}

func (r *roleRepository) Update(ctx context.Context, id uuid.UUID, data *models.UpdateRole) (*models.Role, *errx.Error) {
	setClauses := []string{}
	args := []any{id}
	argPos := 2

	if data.Name != nil {
		if !validate.RoleName(*data.Name) {
			return nil, errx.ErrRoleName
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "name", argPos))
		args = append(args, *data.Name)
		argPos++
	}

	if data.Color != nil {
		if !crypt.IsValidHexColor(*data.Color) {
			return nil, errx.ErrColor
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "color", argPos))
		args = append(args, *data.Color)
		argPos++
	}

	if data.Permissions != nil {
		if *data.Permissions&^models.AllPermissionBits != 0 {
			return nil, errx.ErrBitmask
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "permissions", argPos))
		args = append(args, *data.Permissions)
		argPos++
	}

	var rol models.Role

	query := fmt.Sprintf(`
		UPDATE roles SET %s
		WHERE id = $1
		RETURNING %s`,
		strings.Join(setClauses, ", "),
		RoleSelect,
	)

	row := r.DB.QueryRow(
		ctx,
		query,
		args...,
	)

	if err := GetRole(row, &rol); err != nil {
		db.CaptureError(err, query, args, "queryrow")
		return nil, errx.InternalError()
	}

	return &rol, nil
}

func (r *roleRepository) Delete(ctx context.Context, id uuid.UUID) *errx.Error {
	query := `
		DELETE FROM roles
		WHERE id = $1
	`

	params := []any{
		id,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)

	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}

	return nil
}

func (r *roleRepository) Add(ctx context.Context, userID, roleID uuid.UUID) *errx.Error {
	query := `
		INSERT INTO user_roles (
		 user_id, role_id
		) VALUES (
		 $1, $2
		)
	`

	params := []any{
		userID,
		roleID,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)

	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}

	return nil
}

func (r *roleRepository) Remove(ctx context.Context, userID, roleID uuid.UUID) *errx.Error {
	query := `
		DELETE FROM user_roles
		WHERE user_id = $1 AND role_id = $2
	`

	params := []any{
		userID,
		roleID,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)

	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()

	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}

	return nil
}
