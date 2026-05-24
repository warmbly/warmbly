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
	AuditActionLogin   AuditAction = "login"
	AuditActionLogout  AuditAction = "logout"
	AuditActionAPICall AuditAction = "api_call"
	AuditActionExport  AuditAction = "export"
	AuditActionImport  AuditAction = "import"
	AuditActionRevoke  AuditAction = "revoke"
	AuditActionConnect AuditAction = "connect"

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
	AuditEntitySequence       AuditEntityType = "sequence"
	AuditEntityUser           AuditEntityType = "user"
	AuditEntitySession        AuditEntityType = "session"
	AuditEntityOrganization   AuditEntityType = "organization"
	AuditEntityWorker         AuditEntityType = "worker"
	AuditEntityAWSCredentials AuditEntityType = "aws_credentials"
	AuditEntityWorkerProfile  AuditEntityType = "worker_profile"
	AuditEntityRelease        AuditEntityType = "release"
)

type AuditLog struct {
	ID         uuid.UUID         `json:"id"`
	UserID     uuid.UUID         `json:"user_id"`
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

type AuditLogSearch struct {
	UserID     *uuid.UUID
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

// CreateAuditLog is a helper struct for creating audit logs
type CreateAuditLog struct {
	UserID     uuid.UUID
	Action     AuditAction
	EntityType AuditEntityType
	EntityID   *uuid.UUID
	IPAddress  string
	UserAgent  string
	Changes    map[string]string
	Metadata   map[string]string
}
