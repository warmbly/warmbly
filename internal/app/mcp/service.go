// Package mcp manages org-connected MCP servers (client direction): connect a
// server, discover its tools, and expose the enabled ones to the dashboard
// agent as approval-gated, namespaced tools. Bearer credentials are sealed with
// the org DEK; URLs are SSRF-validated; a server is disabled until an admin
// reviews its tools and turns it on.
package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	mcpclient "github.com/warmbly/warmbly/internal/pkg/mcp"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service manages connected MCP servers and contributes their tools to the
// registry.
type Service interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.MCPServer, *errx.Error)
	Create(ctx context.Context, orgID, createdBy uuid.UUID, req *models.CreateMCPServer) (*models.MCPServer, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, req *models.UpdateMCPServer) (*models.MCPServer, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	// Refresh re-runs tool discovery against a connected server.
	Refresh(ctx context.Context, orgID, id uuid.UUID) (*models.MCPServer, *errx.Error)

	// ToolsForInvocation contributes the org's enabled MCP tools to the agent.
	ToolsForInvocation(ctx context.Context, inv aitools.Invocation) []generation.ToolDef
}

type service struct {
	repo   repository.MCPRepository
	cipher cipher.CipherService
	client *mcpclient.Client
}

func NewService(repo repository.MCPRepository, cipherSvc cipher.CipherService) Service {
	return &service{repo: repo, cipher: cipherSvc, client: mcpclient.NewClient(20 * time.Second)}
}

func (s *service) List(ctx context.Context, orgID uuid.UUID) ([]models.MCPServer, *errx.Error) {
	out, err := s.repo.List(ctx, orgID, false)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list connections")
	}
	return out, nil
}

func (s *service) Create(ctx context.Context, orgID, createdBy uuid.UUID, req *models.CreateMCPServer) (*models.MCPServer, *errx.Error) {
	name := strings.TrimSpace(req.Name)
	url := strings.TrimSpace(req.URL)
	if name == "" || url == "" {
		return nil, errx.New(errx.BadRequest, "name and url are required")
	}
	// SSRF: https only, publicly routable (blocks localhost/metadata/private).
	if err := webhook.ValidateOutboundURL(url); err != nil {
		return nil, errx.New(errx.BadRequest, "invalid server url: "+err.Error())
	}
	auth := req.Auth
	if auth == "" {
		auth = "none"
	}
	if auth != "none" && auth != "bearer" {
		return nil, errx.New(errx.BadRequest, "auth_type must be none or bearer")
	}

	sealed := ""
	if auth == "bearer" {
		if strings.TrimSpace(req.Token) == "" {
			return nil, errx.New(errx.BadRequest, "a bearer token is required")
		}
		var serr *errx.Error
		sealed, serr = s.seal(ctx, orgID, req.Token)
		if serr != nil {
			return nil, serr
		}
	}

	sv := &models.MCPServer{
		OrgID: orgID, Name: name, URL: url, AuthType: auth,
		CredentialsEncrypted: sealed, Enabled: false, CreatedBy: &createdBy,
	}
	// Discover tools before persisting so the admin can review them. Only forward
	// the token for bearer auth (mirrors Refresh/ToolsForInvocation), so a
	// "none" server never puts an Authorization header on the wire. A discovery
	// failure still connects the server (disabled) with the error recorded.
	discoverToken := ""
	if auth == "bearer" {
		discoverToken = req.Token
	}
	s.discover(ctx, sv, discoverToken)

	out, err := s.repo.Create(ctx, sv)
	if err != nil {
		return nil, errx.New(errx.Conflict, "a connection with that name already exists")
	}
	return out, nil
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, req *models.UpdateMCPServer) (*models.MCPServer, *errx.Error) {
	sv, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read connection")
	}
	if sv == nil {
		return nil, errx.ErrNotFound
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			return nil, errx.New(errx.BadRequest, "name is required")
		}
		sv.Name = n
	}
	if req.Token != nil && sv.AuthType == "bearer" {
		sealed, serr := s.seal(ctx, orgID, *req.Token)
		if serr != nil {
			return nil, serr
		}
		sv.CredentialsEncrypted = sealed
	}
	if req.Enabled != nil {
		sv.Enabled = *req.Enabled
	}
	if err := s.repo.Update(ctx, sv); err != nil {
		return nil, errx.New(errx.Conflict, "a connection with that name already exists")
	}
	return sv, nil
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	if err := s.repo.Delete(ctx, orgID, id); err != nil {
		return errx.New(errx.Internal, "failed to delete connection")
	}
	return nil
}

func (s *service) Refresh(ctx context.Context, orgID, id uuid.UUID) (*models.MCPServer, *errx.Error) {
	sv, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read connection")
	}
	if sv == nil {
		return nil, errx.ErrNotFound
	}
	token, _ := s.open(ctx, orgID, sv.CredentialsEncrypted)
	s.discover(ctx, sv, token)
	if err := s.repo.Update(ctx, sv); err != nil {
		return nil, errx.New(errx.Internal, "failed to save connection")
	}
	return sv, nil
}

// discover runs initialize + tools/list and updates the server's tools/error.
func (s *service) discover(ctx context.Context, sv *models.MCPServer, token string) {
	tools, err := s.client.ListTools(ctx, sv.URL, token)
	if err != nil {
		sv.DiscoveredTools = nil
		sv.LastError = err.Error()
		return
	}
	out := make([]models.MCPTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, models.MCPTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	sv.DiscoveredTools = out
	sv.LastError = ""
}

func (s *service) ToolsForInvocation(ctx context.Context, inv aitools.Invocation) []generation.ToolDef {
	servers, err := s.repo.List(ctx, inv.OrgID, true)
	if err != nil {
		return nil
	}
	var defs []generation.ToolDef
	for _, sv := range servers {
		sv := sv
		for _, t := range sv.DiscoveredTools {
			t := t
			var schema map[string]any
			if len(t.InputSchema) > 0 {
				_ = json.Unmarshal(t.InputSchema, &schema)
			}
			defs = append(defs, generation.ToolDef{
				Name:        toolName(sv.Name, t.Name),
				Description: "[external tool via " + sv.Name + "] " + t.Description,
				InputSchema: schema,
				// External tools are always approval-gated and never auto-allowed
				// (the agent's Approve hook excludes mcp_* from always_allow).
				Risk: generation.RiskWrite,
				Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
					token, _ := s.open(ctx, sv.OrgID, sv.CredentialsEncrypted)
					return s.client.CallTool(ctx, sv.URL, token, t.Name, args)
				},
			})
		}
	}
	return defs
}

func (s *service) seal(ctx context.Context, orgID uuid.UUID, plaintext string) (string, *errx.Error) {
	if s.cipher == nil {
		return "", errx.New(errx.ServiceUnavailable, "encryption is unavailable")
	}
	c, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		return "", errx.New(errx.Internal, "failed to seal credentials")
	}
	sealed, err := c.Encrypt(ctx, plaintext)
	if err != nil {
		return "", errx.New(errx.Internal, "failed to seal credentials")
	}
	return sealed, nil
}

func (s *service) open(ctx context.Context, orgID uuid.UUID, sealed string) (string, error) {
	if sealed == "" || s.cipher == nil {
		return "", nil
	}
	c, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		return "", err
	}
	return c.Decrypt(ctx, sealed)
}

var nonToolChar = regexp.MustCompile(`[^a-z0-9]+`)

// toolName namespaces an external tool: mcp_<server>_<tool>, sanitized.
func toolName(server, tool string) string {
	slug := func(v string) string {
		return strings.Trim(nonToolChar.ReplaceAllString(strings.ToLower(v), "_"), "_")
	}
	return "mcp_" + slug(server) + "_" + slug(tool)
}
