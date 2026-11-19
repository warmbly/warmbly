package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// TeamRepository defines data access for org teams and their members. Teams are
// always scoped to an organization; member rows reference org users (membership
// in the org is enforced on write via organization_members).
type TeamRepository interface {
	ListTeams(ctx context.Context, orgID uuid.UUID) ([]models.Team, error)
	GetTeam(ctx context.Context, orgID, teamID uuid.UUID) (*models.Team, error)
	CreateTeam(ctx context.Context, orgID uuid.UUID, data *models.CreateTeam) (*models.Team, error)
	UpdateTeam(ctx context.Context, orgID, teamID uuid.UUID, data *models.UpdateTeam) (*models.Team, error)
	DeleteTeam(ctx context.Context, orgID, teamID uuid.UUID) error

	AddMember(ctx context.Context, orgID, teamID, userID uuid.UUID) error
	RemoveMember(ctx context.Context, orgID, teamID, userID uuid.UUID) error
}

type teamRepository struct {
	db *pgxpool.Pool
}

// NewTeamRepository creates a new team repository.
func NewTeamRepository(db *pgxpool.Pool) TeamRepository {
	return &teamRepository{db: db}
}

// ListTeams returns every team in the org with its members hydrated. Members are
// loaded in one extra query and attached to their owning team, mirroring how
// ListPipelines hydrates stages — so the list view never sees an empty members
// array for a team that actually has members.
func (r *teamRepository) ListTeams(ctx context.Context, orgID uuid.UUID) ([]models.Team, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, name, color, created_at, updated_at
		FROM teams
		WHERE organization_id = $1
		ORDER BY name ASC
		LIMIT 200
	`, orgID)
	if err != nil {
		return nil, err
	}

	teams := []models.Team{}
	indexByID := make(map[uuid.UUID]int)
	for rows.Next() {
		var t models.Team
		if err := rows.Scan(
			&t.ID, &t.OrganizationID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		t.Members = []models.TeamMember{}
		indexByID[t.ID] = len(teams)
		teams = append(teams, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(teams) == 0 {
		return teams, nil
	}

	teamIDs := make([]uuid.UUID, 0, len(teams))
	for _, t := range teams {
		teamIDs = append(teamIDs, t.ID)
	}

	memberRows, err := r.db.Query(ctx, `
		SELECT tm.team_id, tm.user_id, u.email, COALESCE(u.contact_email, ''),
		       TRIM(CONCAT(u.first_name, ' ', u.last_name)), tm.added_at
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = ANY($1)
		ORDER BY tm.added_at ASC
	`, teamIDs)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()

	for memberRows.Next() {
		var teamID uuid.UUID
		var m models.TeamMember
		var contact string
		if err := memberRows.Scan(&teamID, &m.UserID, &m.Email, &contact, &m.Name, &m.AddedAt); err != nil {
			return nil, err
		}
		m.Email = models.VisibleEmail(m.Email, contact)
		if idx, ok := indexByID[teamID]; ok {
			teams[idx].Members = append(teams[idx].Members, m)
		}
	}
	if err := memberRows.Err(); err != nil {
		return nil, err
	}

	return teams, nil
}

// GetTeam returns a single team scoped to the org, with its members hydrated.
func (r *teamRepository) GetTeam(ctx context.Context, orgID, teamID uuid.UUID) (*models.Team, error) {
	var t models.Team
	err := r.db.QueryRow(ctx, `
		SELECT id, organization_id, name, color, created_at, updated_at
		FROM teams
		WHERE organization_id = $1 AND id = $2
	`, orgID, teamID).Scan(
		&t.ID, &t.OrganizationID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}

	members, err := r.listMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	t.Members = members
	return &t, nil
}

// listMembers loads the hydrated member rows for a single team.
func (r *teamRepository) listMembers(ctx context.Context, teamID uuid.UUID) ([]models.TeamMember, error) {
	rows, err := r.db.Query(ctx, `
		SELECT tm.user_id, u.email, COALESCE(u.contact_email, ''),
		       TRIM(CONCAT(u.first_name, ' ', u.last_name)), tm.added_at
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = $1
		ORDER BY tm.added_at ASC
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []models.TeamMember{}
	for rows.Next() {
		var m models.TeamMember
		var contact string
		if err := rows.Scan(&m.UserID, &m.Email, &contact, &m.Name, &m.AddedAt); err != nil {
			return nil, err
		}
		m.Email = models.VisibleEmail(m.Email, contact)
		members = append(members, m)
	}
	return members, rows.Err()
}

// CreateTeam inserts a new team for the org. Color defaults to the slate accent
// when the caller leaves it empty.
func (r *teamRepository) CreateTeam(ctx context.Context, orgID uuid.UUID, data *models.CreateTeam) (*models.Team, error) {
	color := data.Color
	if color == "" {
		color = "#94a3b8"
	}

	var t models.Team
	err := r.db.QueryRow(ctx, `
		INSERT INTO teams (organization_id, name, color)
		VALUES ($1, $2, $3)
		RETURNING id, organization_id, name, color, created_at, updated_at
	`, orgID, data.Name, color).Scan(
		&t.ID, &t.OrganizationID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Members = []models.TeamMember{}
	return &t, nil
}

// UpdateTeam applies a partial update to a team scoped to the org.
func (r *teamRepository) UpdateTeam(ctx context.Context, orgID, teamID uuid.UUID, data *models.UpdateTeam) (*models.Team, error) {
	setClauses := []string{}
	args := []any{orgID, teamID}
	argPos := 3

	if data.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argPos))
		args = append(args, *data.Name)
		argPos++
	}
	if data.Color != nil {
		setClauses = append(setClauses, fmt.Sprintf("color = $%d", argPos))
		args = append(args, *data.Color)
		argPos++
	}

	if len(setClauses) == 0 {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE teams SET %s
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, name, color, created_at, updated_at
	`, strings.Join(setClauses, ", "))

	var t models.Team
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&t.ID, &t.OrganizationID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}

	members, err := r.listMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	t.Members = members
	return &t, nil
}

// DeleteTeam removes a team (and, via cascade, its members) scoped to the org.
func (r *teamRepository) DeleteTeam(ctx context.Context, orgID, teamID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `DELETE FROM teams WHERE organization_id = $1 AND id = $2`, orgID, teamID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

// AddMember adds an org user to a team. The team must belong to the org and the
// user must already be a member of that org (validated against
// organization_members); otherwise the insert is rejected before it runs.
// Re-adding an existing member is a no-op.
func (r *teamRepository) AddMember(ctx context.Context, orgID, teamID, userID uuid.UUID) error {
	// Team must belong to the org.
	var exists bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM teams WHERE id = $1 AND organization_id = $2)`,
		teamID, orgID,
	).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errx.ErrNotFound
	}

	// User must belong to the org.
	var isOrgMember bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM organization_members WHERE organization_id = $1 AND user_id = $2)`,
		orgID, userID,
	).Scan(&isOrgMember); err != nil {
		return err
	}
	if !isOrgMember {
		return errx.New(errx.BadRequest, "user is not a member of this organization")
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO team_members (team_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (team_id, user_id) DO NOTHING
	`, teamID, userID)
	return err
}

// RemoveMember removes a user from a team. The team must belong to the org.
func (r *teamRepository) RemoveMember(ctx context.Context, orgID, teamID, userID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `
		DELETE FROM team_members tm
		USING teams t
		WHERE tm.team_id = t.id
		  AND t.organization_id = $1
		  AND tm.team_id = $2
		  AND tm.user_id = $3
	`, orgID, teamID, userID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}
