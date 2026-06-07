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
	// Attribution: the campaign + sender mailbox that produced the reply this
	// deal was created from. Nullable; editable (best-guess, not ground truth).
	CampaignID      *uuid.UUID `json:"campaign_id,omitempty"`
	SourceMailboxID *uuid.UUID `json:"source_mailbox_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	// Joined (populated by the search/list query, not by single-row reads).
	Contact      *Contact       `json:"contact,omitempty"`
	Stage        *PipelineStage `json:"stage,omitempty"`
	CampaignName *string        `json:"campaign_name,omitempty"`
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
	CampaignID        *uuid.UUID `json:"campaign_id,omitempty"`
	SourceMailboxID   *uuid.UUID `json:"source_mailbox_id,omitempty"`
}

// =====================
// Deal search + summary
// =====================

// SearchDeals is the faceted, server-side filter body shared by
// POST /crm/deals/search and POST /crm/deals/summary. Every facet is
// optional; an empty body matches every deal in the organization. This is
// the surface that makes the deals views correct at scale instead of paging
// 100 rows and reducing client-side.
type SearchDeals struct {
	Query         string     `json:"query"`          // name ILIKE
	Statuses      []string   `json:"statuses"`       // open | won | lost (any of)
	PipelineIDs   []string   `json:"pipeline_ids"`   // any of (cross-pipeline by default)
	StageIDs      []string   `json:"stage_ids"`      // any of
	AssignedTo    []string   `json:"assigned_to"`    // owner is any of
	CampaignIDs   []string   `json:"campaign_ids"`   // attributed campaign is any of
	MinValue      *float64   `json:"min_value"`      // value >=
	MaxValue      *float64   `json:"max_value"`      // value <=
	CloseAfter    *time.Time `json:"close_after"`    // expected_close_date >=
	CloseBefore   *time.Time `json:"close_before"`   // expected_close_date <=
	CreatedAfter  *time.Time `json:"created_after"`  // created_at >=
	CreatedBefore *time.Time `json:"created_before"` // created_at <=
	SortBy        string     `json:"sort_by"`        // created_at|updated_at|value|expected_close_date|name
	Reverse       bool       `json:"reverse"`        // true = ASC, false = DESC (default)
}

// DealsSearchResult is the offset-paginated result of POST /crm/deals/search.
// Offset (not keyset) pagination is used because the sortable columns include
// nullable value/expected_close_date, where a keyset cursor would silently
// drop NULL-valued rows. Total is exact so the UI can show "N of M".
type DealsSearchResult struct {
	Data       []Deal                `json:"data"`
	Pagination DealsSearchPagination `json:"pagination"`
}

type DealsSearchPagination struct {
	Total      int64 `json:"total"`
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
	HasMore    bool  `json:"has_more"`
	NextOffset *int  `json:"next_offset,omitempty"`
}

// DealsSummary is the server-side aggregate over the SAME filter body as a
// search, so every header total and per-stage column total is a true SUM/COUNT
// over the whole matching set — never a client reduce over a truncated page.
type DealsSummary struct {
	Total         int64              `json:"total"`
	OpenCount     int64              `json:"open_count"`
	OpenValue     float64            `json:"open_value"`
	WonCount      int64              `json:"won_count"`
	WonValue      float64            `json:"won_value"`
	LostCount     int64              `json:"lost_count"`
	LostValue     float64            `json:"lost_value"`
	Currency      string             `json:"currency"`
	Stages        []DealStageSummary `json:"stages"`
	MixedCurrency bool               `json:"mixed_currency"`
}

// DealStageSummary feeds an accurate per-column header on the kanban board:
// the count of deals in the stage and the open-deal value in it.
type DealStageSummary struct {
	StageID uuid.UUID `json:"stage_id"`
	Count   int64     `json:"count"`
	Value   float64   `json:"value"`
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

// CRMTaskType is a user-managed task type — the kind of work a task represents
// (e.g. Call / Email / Meeting). Org-scoped. A task references its type by NAME
// (crm_tasks.type), so deleting a type never orphans existing tasks; they keep
// the label and fall back to a neutral colour.
type CRMTaskType struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Name           string    `json:"name"`
	Color          string    `json:"color"`
	Position       int       `json:"position"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateCRMTaskType struct {
	Name  string `json:"name" binding:"required,min=1,max=60"`
	Color string `json:"color,omitempty"`
}

type UpdateCRMTaskType struct {
	Name     *string `json:"name,omitempty"`
	Color    *string `json:"color,omitempty"`
	Position *int    `json:"position,omitempty"`
}

// DefaultCRMTaskTypes seed an org's set the first time its types are listed, so
// there's a usable starting point the user can rename, recolour, or delete.
var DefaultCRMTaskTypes = []CreateCRMTaskType{
	{Name: "Call", Color: "#8b5cf6"},
	{Name: "Email", Color: "#0ea5e9"},
	{Name: "Meeting", Color: "#f59e0b"},
}

type CRMTask struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	ContactID      *uuid.UUID      `json:"contact_id,omitempty"`
	DealID         *uuid.UUID      `json:"deal_id,omitempty"`
	AssignedTo     *uuid.UUID      `json:"assigned_to,omitempty"`
	AssignedTeamID *uuid.UUID      `json:"assigned_team_id,omitempty"`
	CreatedBy      uuid.UUID       `json:"created_by"`
	Title          string          `json:"title"`
	Description    *string         `json:"description,omitempty"`
	DueDate        *time.Time      `json:"due_date,omitempty"`
	Priority       CRMTaskPriority `json:"priority"`
	Type           string          `json:"type"`
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
	ContactID      *uuid.UUID `json:"contact_id,omitempty"`
	DealID         *uuid.UUID `json:"deal_id,omitempty"`
	AssignedTo     *uuid.UUID `json:"assigned_to,omitempty"`
	AssignedTeamID *uuid.UUID `json:"assigned_team_id,omitempty"`
	Title          string     `json:"title" binding:"required,min=1,max=255"`
	Description    *string    `json:"description,omitempty"`
	DueDate        *time.Time `json:"due_date,omitempty"`
	Priority       string     `json:"priority,omitempty"`
	Type           string     `json:"type,omitempty"`
}

type UpdateCRMTask struct {
	AssignedTo     *uuid.UUID `json:"assigned_to,omitempty"`
	AssignedTeamID *uuid.UUID `json:"assigned_team_id,omitempty"`
	Title          *string    `json:"title,omitempty"`
	Description    *string    `json:"description,omitempty"`
	DueDate        *time.Time `json:"due_date,omitempty"`
	Priority       *string    `json:"priority,omitempty"`
	Type           *string    `json:"type,omitempty"`
	Status         *string    `json:"status,omitempty"`
}

// =====================
// Task search + summary
// =====================

// SearchTasks is the faceted, server-side filter body shared by
// POST /crm/tasks/search and POST /crm/tasks/summary. Every facet is optional;
// an empty body matches every task in the organization. This is the surface
// that makes the tasks view correct at scale instead of paging a cursor and
// reducing client-side.
type SearchTasks struct {
	Query      string      `json:"query"`       // title ILIKE
	Statuses   []string    `json:"statuses"`    // pending | in_progress | completed | cancelled (any of)
	Priorities []string    `json:"priorities"`  // low | medium | high | urgent (any of)
	Types      []string    `json:"types"`       // task type NAME is any of
	AssignedTo []string    `json:"assigned_to"` // assignee user id is any of
	TeamIDs    []uuid.UUID `json:"team_ids"`    // task team is any of, OR assignee is a member of any of
	ContactID  *string     `json:"contact_id"`  // linked contact
	DealID     *string     `json:"deal_id"`     // linked deal
	DueAfter   *time.Time  `json:"due_after"`   // due_date >=
	DueBefore  *time.Time  `json:"due_before"`  // due_date <=
	Overdue    bool        `json:"overdue"`     // due_date < now() AND not completed/cancelled
	SortBy     string      `json:"sort_by"`     // created_at|due_date|priority|title|updated_at
	Reverse    bool        `json:"reverse"`     // true = ASC, false = DESC (default)
}

// TasksSearchResult is the offset-paginated result of POST /crm/tasks/search.
// Offset (not keyset) pagination is used because the sortable columns include
// the nullable due_date, where a keyset cursor would silently drop NULL-valued
// rows. Total is exact so the UI can show "N of M".
type TasksSearchResult struct {
	Data       []CRMTask             `json:"data"`
	Pagination TasksSearchPagination `json:"pagination"`
}

type TasksSearchPagination struct {
	Total      int64 `json:"total"`
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
	HasMore    bool  `json:"has_more"`
	NextOffset *int  `json:"next_offset,omitempty"`
}

// TasksSummary is the server-side aggregate over the SAME filter body as a
// search, so every header total is a true COUNT over the whole matching set
// rather than a client reduce over a truncated page.
type TasksSummary struct {
	Total          int64 `json:"total"`
	PendingCount   int64 `json:"pending_count"`
	InProgress     int64 `json:"in_progress_count"`
	CompletedCount int64 `json:"completed_count"`
	CancelledCount int64 `json:"cancelled_count"`
	OverdueCount   int64 `json:"overdue_count"`
	HighPriority   int64 `json:"high_priority_count"`
}
