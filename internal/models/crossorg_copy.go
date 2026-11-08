package models

import "github.com/google/uuid"

// Bounds on one copy-to-workspace request: each destination is its own
// transaction and its own blob copies, so the request stays a short one.
const (
	CopyToWorkspacesMax = 10
	CopyTemplatesMax    = 50
)

// Outcome of one destination in a copy-to-workspace request.
const (
	WorkspaceCopyCopied  = "copied"
	WorkspaceCopyPartial = "partial"
	WorkspaceCopyFailed  = "failed"
)

// CopyCampaignToWorkspacesRequest is the body of POST /campaigns/:id/copy-to.
// TemplateIDs are reply templates of the source workspace that travel along.
type CopyCampaignToWorkspacesRequest struct {
	OrganizationIDs []uuid.UUID `json:"organization_ids"`
	TemplateIDs     []uuid.UUID `json:"template_ids,omitempty"`
}

// CopyTemplatesToWorkspacesRequest is the body of POST /templates/copy-to.
type CopyTemplatesToWorkspacesRequest struct {
	TemplateIDs     []uuid.UUID `json:"template_ids"`
	OrganizationIDs []uuid.UUID `json:"organization_ids"`
}

// CopiedRef names one row a copy created in the destination.
type CopiedRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// CopyReview is a copied step whose settings pointed at something only the
// source workspace has (a tag, segment, pipeline, automation, teammate or
// form). Fields names them; the copy clears every one except a form link,
// which resolves to nothing in the destination until it is replaced.
type CopyReview struct {
	StepID   uuid.UUID `json:"step_id"`
	StepName string    `json:"step_name"`
	Fields   []string  `json:"fields"`
}

// CopyFailure is the stable code and message of a destination that failed.
type CopyFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WorkspaceCopyResult is what one destination workspace received.
type WorkspaceCopyResult struct {
	OrganizationID   uuid.UUID    `json:"organization_id"`
	OrganizationName string       `json:"organization_name"`
	Status           string       `json:"status"`
	Campaign         *CopiedRef   `json:"campaign,omitempty"`
	Templates        []CopiedRef  `json:"templates"`
	Review           []CopyReview `json:"review"`
	Warnings         []string     `json:"warnings"`
	Error            *CopyFailure `json:"error,omitempty"`
}

// WorkspaceCopyResponse answers a copy-to-workspace request, one entry per
// destination in the order they were asked for.
type WorkspaceCopyResponse struct {
	Data []WorkspaceCopyResult `json:"data"`
}
