package models

import (
	"time"

	"github.com/google/uuid"
)

type AuditAction string

const (
	AuditActionCreate  AuditAction = "create"
	AuditActionUpdate  AuditAction = "update"
	AuditActionDelete  AuditAction = "delete"
	AuditActionAPICall AuditAction = "api_call"
	AuditActionExport  AuditAction = "export"
	AuditActionImport  AuditAction = "import"
	AuditActionRevoke  AuditAction = "revoke"
	AuditActionConnect AuditAction = "connect"

	// Lifecycle / workflow actions
	AuditActionStart      AuditAction = "start"
	AuditActionStop       AuditAction = "stop"
	AuditActionPause      AuditAction = "pause"
	AuditActionResume     AuditAction = "resume"
	AuditActionSend       AuditAction = "send"
	AuditActionDuplicate  AuditAction = "duplicate"
	AuditActionDisconnect AuditAction = "disconnect"
	AuditActionRotate     AuditAction = "rotate"

	// Membership / governance actions
	AuditActionInvite   AuditAction = "invite"
	AuditActionRemove   AuditAction = "remove"
	AuditActionTransfer AuditAction = "transfer"

	// Worker / fleet operations
	AuditActionTest          AuditAction = "test"
	AuditActionInstall       AuditAction = "install"
	AuditActionRestart       AuditAction = "restart"
	AuditActionUpgrade       AuditAction = "upgrade"
	AuditActionUninstall     AuditAction = "uninstall"
	AuditActionRotateKeys    AuditAction = "rotate_keys"
	AuditActionApply         AuditAction = "apply"
	AuditActionAssign        AuditAction = "assign"
	AuditActionSystemUpdate  AuditAction = "system_update"
	AuditActionReboot        AuditAction = "reboot"
	AuditActionCheckReleases AuditAction = "check_releases"
)

type AuditEntityType string

const (
	AuditEntityCampaign       AuditEntityType = "campaign"
	AuditEntityContact        AuditEntityType = "contact"
	AuditEntityEmailAccount   AuditEntityType = "email_account"
	AuditEntityAPIKey         AuditEntityType = "api_key"
	AuditEntitySequence       AuditEntityType = "step"
	AuditEntityUser           AuditEntityType = "user"
	AuditEntityOrganization   AuditEntityType = "organization"
	AuditEntityWorker         AuditEntityType = "worker"
	AuditEntityAWSCredentials AuditEntityType = "aws_credentials"
	AuditEntityWorkerProfile  AuditEntityType = "worker_profile"
	AuditEntityRelease        AuditEntityType = "release"

	// Org-scoped configuration & governance entities
	AuditEntityOrganizationMember AuditEntityType = "organization_member"
	AuditEntityInvitation         AuditEntityType = "invitation"
	AuditEntityTemplate           AuditEntityType = "template"
	AuditEntityWebhook            AuditEntityType = "webhook"
	AuditEntityIntegration        AuditEntityType = "integration"
	AuditEntityWarmupRoutingRule  AuditEntityType = "warmup_routing_rule"
	AuditEntityFolder             AuditEntityType = "folder"
	AuditEntityTag                AuditEntityType = "tag"
	AuditEntityCategory           AuditEntityType = "category"
	AuditEntitySubscription       AuditEntityType = "subscription"
	AuditEntitySettings           AuditEntityType = "settings"

	// CRM entities
	AuditEntityCRMPipeline AuditEntityType = "crm_pipeline"
	AuditEntityCRMStage    AuditEntityType = "crm_stage"
	AuditEntityCRMDeal     AuditEntityType = "crm_deal"
	AuditEntityCRMTask     AuditEntityType = "crm_task"
	AuditEntityCRMNote     AuditEntityType = "crm_note"

	// Inbox
	AuditEntityUnibox AuditEntityType = "unibox"

	// Collaboration / automation surfaces
	AuditEntityTeam           AuditEntityType = "team"
	AuditEntityAutomation     AuditEntityType = "automation"
	AuditEntityLeadSyncSource AuditEntityType = "lead_sync_source"
	AuditEntityMeeting        AuditEntityType = "meeting"
	AuditEntityRole           AuditEntityType = "role"
)

// AuditActor is the minimal identity of the member who performed an action,
// resolved by joining the users table so the dashboard can render "who"
// instead of a bare UUID. Nil when the acting user has since been deleted.
type AuditActor struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
}

type AuditLog struct {
	ID         uuid.UUID         `json:"id"`
	OrgID      uuid.UUID         `json:"org_id"`
	UserID     uuid.UUID         `json:"user_id"` // actor id; kept for backwards-compatible JSON
	Actor      *AuditActor       `json:"actor,omitempty"`
	ActionDate time.Time         `json:"action_date"`
	Action     AuditAction       `json:"action"`
	EntityType AuditEntityType   `json:"entity_type"`
	EntityID   *uuid.UUID        `json:"entity_id,omitempty"`
	IPAddress  string            `json:"ip_address"`
	UserAgent  string            `json:"user_agent"`
	Changes    map[string]string `json:"changes,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

// AuditLogSearch filters an organization's audit trail. OrgID is required and
// is always set server-side from the caller's session — never from a
// client-supplied value — so one organization can never read another's trail.
type AuditLogSearch struct {
	OrgID      *uuid.UUID
	ActorID    *uuid.UUID
	EntityType *AuditEntityType
	EntityID   *uuid.UUID
	Action     *AuditAction
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Cursor     string
}

type AuditLogsResult struct {
	Data       []AuditLog  `json:"data"`
	Pagination CPagination `json:"pagination"`
}

// CreateAuditLog is a helper struct for creating audit logs.
type CreateAuditLog struct {
	OrgID      uuid.UUID
	UserID     uuid.UUID // actor id
	Action     AuditAction
	EntityType AuditEntityType
	EntityID   *uuid.UUID
	IPAddress  string
	UserAgent  string
	Changes    map[string]string
	Metadata   map[string]string
}
