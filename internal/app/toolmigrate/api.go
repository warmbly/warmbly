// Package toolmigrate brings a workspace's data over from another outreach
// tool: blocklist, mailboxes, campaigns and leads, for a workspace or for an
// agency's client workspaces.
package toolmigrate

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// Actor is who asks, and through what. Mailbox passwords from the source are
// only used when a signed-in person started the migration.
type Actor struct {
	UserID uuid.UUID
	// OrgPerms are the caller's permissions in the workspace (workspace
	// migrations only; agency migrations check each target's membership).
	OrgPerms models.OrganizationPermission
	// ViaSession is true for a signed-in dashboard session, false for any key or token.
	ViaSession bool
}

// Scope is whose migrations a call reaches: exactly one of the two is set.
type Scope struct {
	OrgID    *uuid.UUID
	AgencyID *uuid.UUID
}

// API is what handlers, MCP tools and the CLI call. Every method is scoped:
// a migration outside the scope reads as not found.
type API interface {
	Providers() []source.Info
	Create(ctx context.Context, scope Scope, actor Actor, in models.NewToolMigration) (*models.ToolMigration, *errx.Error)
	List(ctx context.Context, scope Scope, cursor string, limit int) (*models.ToolMigrationList, *errx.Error)
	Get(ctx context.Context, scope Scope, id uuid.UUID) (*models.ToolMigration, *errx.Error)
	Start(ctx context.Context, scope Scope, actor Actor, id uuid.UUID, in models.StartToolMigration) (*models.ToolMigration, *errx.Error)
	Cancel(ctx context.Context, scope Scope, actor Actor, id uuid.UUID) (*models.ToolMigration, *errx.Error)
	LinkSenders(ctx context.Context, scope Scope, actor Actor, id uuid.UUID) (*models.ToolMigration, *errx.Error)
	Dismiss(ctx context.Context, scope Scope, actor Actor, id uuid.UUID) *errx.Error
}
