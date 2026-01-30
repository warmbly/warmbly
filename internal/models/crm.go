package models

import (
	"time"

	"github.com/google/uuid"
)

// =====================
// Contact Notes
// =====================

type ContactNote struct {
	ID             uuid.UUID `json:"id"`
	ContactID      uuid.UUID `json:"contact_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	UserID         uuid.UUID `json:"user_id"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	// Joined
	User *User `json:"user,omitempty"`
}

type ContactNotesResult struct {
	Data       []ContactNote `json:"data"`
	Pagination Pagination    `json:"pagination"`
}

type CreateContactNote struct {
	Content string `json:"content" binding:"required,max=10000"`
}

type UpdateContactNote struct {
	Content *string `json:"content,omitempty"`
}

// =====================
// Contact Activities
// =====================

type ActivityType string

const (
	ActivityEmailSent       ActivityType = "email_sent"
	ActivityEmailOpened     ActivityType = "email_opened"
	ActivityEmailClicked    ActivityType = "email_clicked"
	ActivityEmailReplied    ActivityType = "email_replied"
	ActivityEmailBounced    ActivityType = "email_bounced"
	ActivityNoteAdded       ActivityType = "note_added"
	ActivityNoteUpdated     ActivityType = "note_updated"
	ActivityDealCreated     ActivityType = "deal_created"
	ActivityDealStageChange ActivityType = "deal_stage_changed"
	ActivityDealWon         ActivityType = "deal_won"
	ActivityDealLost        ActivityType = "deal_lost"
	ActivityTaskCreated     ActivityType = "task_created"
	ActivityTaskCompleted   ActivityType = "task_completed"
	ActivityContactCreated  ActivityType = "contact_created"
	ActivityContactUpdated  ActivityType = "contact_updated"
	ActivityCampaignAdded   ActivityType = "campaign_added"
	ActivityCampaignRemoved ActivityType = "campaign_removed"
)

type ContactActivity struct {
	ID             uuid.UUID              `json:"id"`
	ContactID      uuid.UUID              `json:"contact_id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	UserID         *uuid.UUID             `json:"user_id,omitempty"`
	ActivityType   ActivityType           `json:"activity_type"`
	Metadata       map[string]interface{} `json:"metadata"`
	CreatedAt      time.Time              `json:"created_at"`
	// Joined
	User *User `json:"user,omitempty"`
}

type ContactActivitiesResult struct {
	Data       []ContactActivity `json:"data"`
	Pagination Pagination        `json:"pagination"`
}

// =====================
// Pipelines
// =====================

type Pipeline struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	Name           string          `json:"name"`
	Position       int             `json:"position"`
	Stages         []PipelineStage `json:"stages,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type PipelineStage struct {
	ID         uuid.UUID `json:"id"`
	PipelineID uuid.UUID `json:"pipeline_id"`
	Name       string    `json:"name"`
	Color      string    `json:"color"`
	Position   int       `json:"position"`
	DealCount  int       `json:"deal_count,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CreatePipeline struct {
	Name   string                `json:"name" binding:"required,min=1,max=255"`
	Stages []CreatePipelineStage `json:"stages"`
}

type CreatePipelineStage struct {
	Name  string `json:"name" binding:"required,min=1,max=255"`
	Color string `json:"color" binding:"required"`
}

type UpdatePipeline struct {
	Name *string `json:"name,omitempty"`
}

type UpdatePipelineStage struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

// =====================
// Deals
// =====================

type DealStatus string

const (
	DealStatusOpen DealStatus = "open"
	DealStatusWon  DealStatus = "won"
	DealStatusLost DealStatus = "lost"
)

type Deal struct {
	ID                uuid.UUID  `json:"id"`
	OrganizationID    uuid.UUID  `json:"organization_id"`
	PipelineID        uuid.UUID  `json:"pipeline_id"`
	StageID           uuid.UUID  `json:"stage_id"`
	ContactID         *uuid.UUID `json:"contact_id,omitempty"`
	Name              string     `json:"name"`
	Value             *float64   `json:"value,omitempty"`
	Currency          string     `json:"currency"`
	Status            DealStatus `json:"status"`
	ExpectedCloseDate *time.Time `json:"expected_close_date,omitempty"`
	WonAt             *time.Time `json:"won_at,omitempty"`
	LostAt            *time.Time `json:"lost_at,omitempty"`
	LostReason        *string    `json:"lost_reason,omitempty"`
	AssignedTo        *uuid.UUID `json:"assigned_to,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	// Joined
	Contact *Contact       `json:"contact,omitempty"`
	Stage   *PipelineStage `json:"stage,omitempty"`
}

type DealsResult struct {
	Data       []Deal     `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type CreateDeal struct {
	PipelineID        uuid.UUID  `json:"pipeline_id" binding:"required"`
	StageID           uuid.UUID  `json:"stage_id" binding:"required"`
	ContactID         *uuid.UUID `json:"contact_id,omitempty"`
	Name              string     `json:"name" binding:"required,min=1,max=255"`
	Value             *float64   `json:"value,omitempty"`
	Currency          string     `json:"currency,omitempty"`
	ExpectedCloseDate *time.Time `json:"expected_close_date,omitempty"`
	AssignedTo        *uuid.UUID `json:"assigned_to,omitempty"`
}

type UpdateDeal struct {
	StageID           *uuid.UUID `json:"stage_id,omitempty"`
	ContactID         *uuid.UUID `json:"contact_id,omitempty"`
	Name              *string    `json:"name,omitempty"`
	Value             *float64   `json:"value,omitempty"`
	Currency          *string    `json:"currency,omitempty"`
	Status            *string    `json:"status,omitempty"`
	ExpectedCloseDate *time.Time `json:"expected_close_date,omitempty"`
	LostReason        *string    `json:"lost_reason,omitempty"`
	AssignedTo        *uuid.UUID `json:"assigned_to,omitempty"`
}

// =====================
// CRM Tasks
// =====================

type CRMTaskPriority string

const (
	CRMTaskPriorityLow    CRMTaskPriority = "low"
	CRMTaskPriorityMedium CRMTaskPriority = "medium"
	CRMTaskPriorityHigh   CRMTaskPriority = "high"
	CRMTaskPriorityUrgent CRMTaskPriority = "urgent"
)

type CRMTaskStatus string

const (
	CRMTaskStatusPending    CRMTaskStatus = "pending"
	CRMTaskStatusInProgress CRMTaskStatus = "in_progress"
	CRMTaskStatusCompleted  CRMTaskStatus = "completed"
	CRMTaskStatusCancelled  CRMTaskStatus = "cancelled"
)

type CRMTask struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	ContactID      *uuid.UUID      `json:"contact_id,omitempty"`
	DealID         *uuid.UUID      `json:"deal_id,omitempty"`
	AssignedTo     *uuid.UUID      `json:"assigned_to,omitempty"`
	CreatedBy      uuid.UUID       `json:"created_by"`
	Title          string          `json:"title"`
	Description    *string         `json:"description,omitempty"`
	DueDate        *time.Time      `json:"due_date,omitempty"`
	Priority       CRMTaskPriority `json:"priority"`
	Status         CRMTaskStatus   `json:"status"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CRMTasksResult struct {
	Data       []CRMTask  `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type CreateCRMTask struct {
	ContactID   *uuid.UUID `json:"contact_id,omitempty"`
	DealID      *uuid.UUID `json:"deal_id,omitempty"`
	AssignedTo  *uuid.UUID `json:"assigned_to,omitempty"`
	Title       string     `json:"title" binding:"required,min=1,max=255"`
	Description *string    `json:"description,omitempty"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	Priority    string     `json:"priority,omitempty"`
}

type UpdateCRMTask struct {
	AssignedTo  *uuid.UUID `json:"assigned_to,omitempty"`
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	Priority    *string    `json:"priority,omitempty"`
	Status      *string    `json:"status,omitempty"`
}
