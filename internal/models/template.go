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
