package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// MCPRepository persists org-connected MCP servers.
type MCPRepository interface {
	Create(ctx context.Context, s *models.MCPServer) (*models.MCPServer, error)
	Update(ctx context.Context, s *models.MCPServer) error
	Delete(ctx context.Context, orgID, id uuid.UUID) error
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.MCPServer, error)
	List(ctx context.Context, orgID uuid.UUID, enabledOnly bool) ([]models.MCPServer, error)
}

type mcpRepository struct {
	DB *db.DB
}

func NewMCPRepository(database *db.DB) MCPRepository {
	return &mcpRepository{DB: database}
}

const mcpCols = `id, org_id, name, url, auth_type, credentials_encrypted, enabled, discovered_tools, last_error, created_by, created_at, updated_at, search_tool`

func scanMCP(row pgx.Row, s *models.MCPServer) error {
	var toolsRaw []byte
	if err := row.Scan(&s.ID, &s.OrgID, &s.Name, &s.URL, &s.AuthType, &s.CredentialsEncrypted, &s.Enabled, &toolsRaw, &s.LastError, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt, &s.SearchTool); err != nil {
		return err
	}
	if len(toolsRaw) > 0 {
		_ = json.Unmarshal(toolsRaw, &s.DiscoveredTools)
	}
	return nil
}

func (r *mcpRepository) Create(ctx context.Context, s *models.MCPServer) (*models.MCPServer, error) {
	toolsRaw, err := json.Marshal(s.DiscoveredTools)
	if err != nil {
		return nil, err
	}
	out := &models.MCPServer{}
	err = scanMCP(r.DB.QueryRow(ctx, `
		INSERT INTO ai_mcp_servers (org_id, name, url, auth_type, credentials_encrypted, enabled, discovered_tools, last_error, created_by, search_tool)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+mcpCols,
		s.OrgID, s.Name, s.URL, s.AuthType, s.CredentialsEncrypted, s.Enabled, toolsRaw, s.LastError, s.CreatedBy, s.SearchTool), out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *mcpRepository) Update(ctx context.Context, s *models.MCPServer) error {
	toolsRaw, err := json.Marshal(s.DiscoveredTools)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(ctx, `
		UPDATE ai_mcp_servers
		SET name = $3, url = $4, auth_type = $5, credentials_encrypted = $6, enabled = $7,
		    discovered_tools = $8, last_error = $9, search_tool = $10, updated_at = now()
		WHERE id = $1 AND org_id = $2`,
		s.ID, s.OrgID, s.Name, s.URL, s.AuthType, s.CredentialsEncrypted, s.Enabled, toolsRaw, s.LastError, s.SearchTool)
	return err
}

func (r *mcpRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `DELETE FROM ai_mcp_servers WHERE id = $1 AND org_id = $2`, id, orgID)
	return err
}

func (r *mcpRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.MCPServer, error) {
	s := &models.MCPServer{}
	err := scanMCP(r.DB.QueryRow(ctx, `SELECT `+mcpCols+` FROM ai_mcp_servers WHERE id = $1 AND org_id = $2`, id, orgID), s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (r *mcpRepository) List(ctx context.Context, orgID uuid.UUID, enabledOnly bool) ([]models.MCPServer, error) {
	query := `SELECT ` + mcpCols + ` FROM ai_mcp_servers WHERE org_id = $1`
	if enabledOnly {
		query += ` AND enabled = true`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.DB.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MCPServer, 0)
	for rows.Next() {
		var s models.MCPServer
		if err := scanMCP(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
