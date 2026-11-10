package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MCPServer is an org-connected external MCP server (client direction). Its
// discovered tools are exposed to the AI assistant only while Enabled.
type MCPServer struct {
	ID              uuid.UUID `json:"id"`
	OrgID           uuid.UUID `json:"org_id"`
	Name            string    `json:"name"`
	URL             string    `json:"url"`
	AuthType        string    `json:"auth_type"`
	Enabled         bool      `json:"enabled"`
	DiscoveredTools []MCPTool `json:"discovered_tools"`
	LastError       string    `json:"last_error,omitempty"`
	// SearchTool names the discovered tool the workspace uses as its web
	// search (the lead finder, and the assistant's search_web); empty for none.
	SearchTool string     `json:"search_tool"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	// CredentialsEncrypted is never serialized to clients.
	CredentialsEncrypted string `json:"-"`
}

// MCPTool is one tool discovered from a connected MCP server.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	// SearchCapable says the tool takes a search query, so it can be picked as
	// the web search. Worked out on every read, never trusted from storage.
	SearchCapable bool `json:"search_capable,omitempty"`
}

// CreateMCPServer is the connect payload.
type CreateMCPServer struct {
	Name  string `json:"name" binding:"required"`
	URL   string `json:"url" binding:"required"`
	Auth  string `json:"auth_type"` // none | bearer
	Token string `json:"token"`     // bearer token, sealed server-side
}

// UpdateMCPServer patches a server; nil fields unchanged.
type UpdateMCPServer struct {
	Name    *string `json:"name"`
	Enabled *bool   `json:"enabled"`
	Token   *string `json:"token"`
	// SearchTool picks one of the server's tools as the workspace's web
	// search; an empty string clears it.
	SearchTool *string `json:"search_tool"`
}
