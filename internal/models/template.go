package models

import (
	"time"

	"github.com/google/uuid"
)

type ReplyTemplate struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	UserID         uuid.UUID `json:"user_id"`
	Name           string    `json:"name"`
	Subject        string    `json:"subject"`
	BodyHTML       string    `json:"body_html"`
	BodyPlain      string    `json:"body_plain"`
	Position       int       `json:"position"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateReplyTemplate struct {
	Name      string `json:"name" binding:"required,max=255"`
	Subject   string `json:"subject"`
	BodyHTML  string `json:"body_html"`
	BodyPlain string `json:"body_plain"`
}

type UpdateReplyTemplate struct {
	Name      *string `json:"name"`
	Subject   *string `json:"subject"`
	BodyHTML  *string `json:"body_html"`
	BodyPlain *string `json:"body_plain"`
}

type ReplyTemplatesResult struct {
	Data []ReplyTemplate `json:"data"`
}

// ListReplyTemplatesQuery accepts optional ?q= search filter that matches
// against name and subject (case-insensitive).
type ListReplyTemplatesQuery struct {
	Search string `form:"q"`
}

// ReorderReplyTemplates carries the new ordering for an organization's
// reply templates. Every ID in IDs is repositioned in the listed order
// (1-indexed). Templates not in the list are left untouched.
type ReorderReplyTemplates struct {
	IDs []uuid.UUID `json:"ids" binding:"required"`
}

// RenderReplyTemplateRequest expands template variables (e.g. {{.FirstName}})
// against an arbitrary key/value map. Used by Unibox to preview a reply.
type RenderReplyTemplateRequest struct {
	Variables map[string]string `json:"variables"`
}

// RenderedReplyTemplate is the rendered output returned to the client.
type RenderedReplyTemplate struct {
	Subject   string `json:"subject"`
	BodyHTML  string `json:"body_html"`
	BodyPlain string `json:"body_plain"`
}
