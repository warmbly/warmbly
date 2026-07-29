package models

import (
	"time"

	"github.com/google/uuid"
)

// MaxSkillContentBytes caps a skill's markdown body.
const MaxSkillContentBytes = 32 * 1024

// AISkill is an org-authored playbook the AI features can load and follow.
type AISkill struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateAISkill is the write payload for a new skill.
type CreateAISkill struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Enabled     *bool  `json:"enabled"`
}

// UpdateAISkill patches a skill; nil fields are left unchanged.
type UpdateAISkill struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
	Enabled     *bool   `json:"enabled"`
}
