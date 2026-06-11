package models

import (
	"strings"

	"database/sql/driver"
	"fmt"
)

// OrganizationPermission represents a bitmask of permissions for organization members.
//
// The DB column is SMALLINT (signed int16), but this type is uint16 so the
// full 16-bit space is usable (RoleOwner == 0xFFFF). The Value/Scan pair
// reinterprets the bits between int16 and uint16 so a high-bit-set value
// like 0xFFFF round-trips as -1 on disk and back, without pgx rejecting
// either direction.
type OrganizationPermission uint16

// Value implements driver.Valuer. Reinterpret the uint16 bits as int16 so
// pgx accepts values > 32767 when writing into a SMALLINT column.
func (p OrganizationPermission) Value() (driver.Value, error) {
	return int64(int16(p)), nil
}

// Scan implements sql.Scanner. The SMALLINT column comes back as int (any
// width depending on driver); reinterpret the low 16 bits as uint16 so a
// stored -1 reads as 0xFFFF.
func (p *OrganizationPermission) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*p = 0
	case int64:
		*p = OrganizationPermission(uint16(v))
	case int32:
		*p = OrganizationPermission(uint16(v))
	case int16:
		*p = OrganizationPermission(uint16(v))
	default:
		return fmt.Errorf("OrganizationPermission.Scan: unsupported type %T", src)
	}
	return nil
}

const (
	// PermManageTeam allows inviting/removing members
	PermManageTeam OrganizationPermission = 1 << iota
	// PermManageBilling allows viewing invoices and upgrading plans
	PermManageBilling
	// PermManageCampaigns allows creating/editing campaigns
	PermManageCampaigns
	// PermManageContacts allows creating/editing contacts
	PermManageContacts
	// PermManageEmails allows connecting email accounts
	PermManageEmails
	// PermViewAnalytics allows viewing reports
	PermViewAnalytics
	// PermSendCampaigns allows starting campaigns
	PermSendCampaigns
	// PermAccessUnibox allows using unified inbox
	PermAccessUnibox
	// PermManageSequences allows creating/editing sequences
	PermManageSequences
	// PermManageSettings allows changing org settings
	PermManageSettings
	// PermViewCampaigns allows read-only campaign access
	PermViewCampaigns
	// PermViewContacts allows read-only contact access
	PermViewContacts
	// PermTransferOwnership allows transferring org ownership
	PermTransferOwnership
	// PermManageAPIKeys allows managing organization API keys
	PermManageAPIKeys
	// PermUseIntegrations allows operating connected integrations (e.g. pushing
	// contacts/deals to a connected CRM) without granting full settings access.
	// This is the operational counterpart to PermManageSettings, which still
	// gates connecting/disconnecting and editing integration configuration.
	PermUseIntegrations
)

// Role represents predefined permission sets
type Role string

const (
	RoleOwner   Role = "owner"
	RoleAdmin   Role = "admin"
	RoleManager Role = "manager"
	RoleViewer  Role = "viewer"
)

// AllPermissions represents all permissions combined
const AllPermissions OrganizationPermission = 0xFFFF

// RolePermissions maps roles to their default permissions
var RolePermissions = map[Role]OrganizationPermission{
	RoleOwner: AllPermissions,
	RoleAdmin: AllPermissions ^ PermTransferOwnership ^ 0, // Admin gets all except transfer
	RoleManager: PermManageCampaigns | PermManageContacts | PermManageEmails |
		PermSendCampaigns | PermManageSequences | PermViewAnalytics |
		PermViewCampaigns | PermViewContacts | PermAccessUnibox |
		PermUseIntegrations,
	RoleViewer: PermViewCampaigns | PermViewContacts | PermViewAnalytics,
}

// IsReservedRoleName reports whether a role name collides with the owner
// status (case-insensitive). Owner is a membership flag, never a role row.
func IsReservedRoleName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), string(RoleOwner))
}

// SeedRole is one of the default roles minted for every new workspace.
// They are ordinary rows afterwards: renameable, editable, deletable.
type SeedRole struct {
	Name        string
	Description string
	Color       string
	Permissions OrganizationPermission
}

// DefaultSeedRoles returns the roles seeded at organization creation,
// mirroring migration 000043 for orgs created after it ran.
func DefaultSeedRoles() []SeedRole {
	allDefined := PermManageTeam | PermManageBilling | PermManageCampaigns | PermManageContacts |
		PermManageEmails | PermViewAnalytics | PermSendCampaigns | PermAccessUnibox |
		PermManageSequences | PermManageSettings | PermViewCampaigns | PermViewContacts |
		PermManageAPIKeys | PermUseIntegrations
	return []SeedRole{
		{Name: "Admin", Description: "Everything except transferring ownership.", Color: "#8b5cf6", Permissions: allDefined},
		{Name: "Manager", Description: "Runs campaigns, contacts, mailboxes, and integrations. No team, billing, or settings access.", Color: "#10b981", Permissions: GetRolePermissions(RoleManager)},
		{Name: "Viewer", Description: "Read-only access to campaigns, contacts, and reports.", Color: "#f59e0b", Permissions: GetRolePermissions(RoleViewer)},
	}
}

// GetRolePermissions returns the default permissions for a role
func GetRolePermissions(role Role) OrganizationPermission {
	if perms, ok := RolePermissions[role]; ok {
		return perms
	}
	return RolePermissions[RoleViewer]
}

// HasPermission checks if the permission set includes a specific permission
func (p OrganizationPermission) HasPermission(perm OrganizationPermission) bool {
	return p&perm == perm
}

// AddPermission adds a permission to the set
func (p OrganizationPermission) AddPermission(perm OrganizationPermission) OrganizationPermission {
	return p | perm
}

// RemovePermission removes a permission from the set
func (p OrganizationPermission) RemovePermission(perm OrganizationPermission) OrganizationPermission {
	return p &^ perm
}

// IsValidRole checks if a role string is valid
func IsValidRole(role string) bool {
	switch Role(role) {
	case RoleOwner, RoleAdmin, RoleManager, RoleViewer:
		return true
	default:
		return false
	}
}
