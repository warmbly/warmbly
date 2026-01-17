package models

import (
	"time"

	"github.com/google/uuid"
)

type Role struct {
	ID          uuid.UUID `json:"id"`
	Permissions uint8     `json:"permissions"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Permission struct {
	Name        string `json:"name"`
	Value       uint8  `json:"value"`
	Description string `json:"description"`
}

const (
	PermManagePlans uint8 = 1 << iota
	PermManageRoles
	PermManageUsers
	PermManageServers
)

var AllPermissions = []Permission{
	{"MANAGE_PLANS", PermManagePlans, "Add, remove or modify plans or change visibility."},
	{"MANAGE_ROLES", PermManageRoles, "Add, remove, modify and manage roles & permissions."},
	{"MANAGE_USERS", PermManageUsers, "Ban, unban and manage users."},
	{"MANAGE_SERVERS", PermManageServers, "Ban, unban and manage users."},
}

var AllPermissionBits uint8 = PermManagePlans | PermManageRoles | PermManageUsers

type RoleData struct {
	Roles       []Role       `json:"roles"`
	Permissions []Permission `json:"permission"`
}

type UpdateRole struct {
	Name        *string `json:"name"`
	Permissions *uint8  `json:"permissions"`
	Color       *string `json:"color"`
}

type CreateRole struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}
