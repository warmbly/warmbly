package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID uuid.UUID `json:"id"`

	FirstName string      `json:"first_name"`
	LastName  string      `json:"last_name"`
	Email     string      `json:"email"`
	AvatarURL *string     `json:"avatar_url,omitempty"`
	Roles     []uuid.UUID `json:"roles"`

	ReferralSource        *string    `json:"referral_source"`
	OnboardingCompletedAt *time.Time `json:"onboarding_completed_at"`

	MaxOrganizations int  `json:"max_organizations"`
	FreeTrialUsed    bool `json:"free_trial_used"`

	// Seconds an instant send is held before it actually leaves, so
	// the user can still cancel it. Bounds live in config
	// (UndoSendSecondsMin/Max); default 30.
	UndoSendSeconds int `json:"undo_send_seconds"`

	// Platform admin access, populated on the /auth/me load. AdminPermissions
	// is the raw bitmask from users.admin_permissions; IsAdmin is the derived
	// "has any admin permission" flag the admin app gates on.
	AdminPermissions AdminPermission `json:"admin_permissions"`
	IsAdmin          bool            `json:"is_admin"`

	// Set when the user has scheduled their own account for deletion.
	// While these are populated the account is "pending deletion" and
	// gets hard-deleted at DeletionScheduledFor unless cancelled.
	DeletionScheduledAt  *time.Time `json:"deletion_scheduled_at,omitempty"`
	DeletionScheduledFor *time.Time `json:"deletion_scheduled_for,omitempty"`

	// Per-user label groups. Always serialized as arrays (never null)
	// so the frontend can iterate without optional-chaining every
	// access. Populated by the /auth/me handler after the base user
	// load.
	Folders    []Group `json:"folders"`
	Tags       []Group `json:"tags"`
	Categories []Group `json:"categories"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsPendingDeletion reports whether the user has a pending account deletion.
func (u *User) IsPendingDeletion() bool {
	return u.DeletionScheduledFor != nil
}
