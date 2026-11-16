package toolmigrate

import (
	"slices"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// ActorFor is the actor for a workspace caller, the one rule REST and the tools share.
// A key or app writes campaigns and mailboxes only with the scopes for them, and only
// a signed-in dashboard session lets the migration use the source's mailbox passwords.
func ActorFor(userID uuid.UUID, orgPerms models.OrganizationPermission, apiPerms uint64, isKey, viaDashboardSession bool) Actor {
	if isKey {
		if !models.HasAPIPermission(apiPerms, models.APIPermWriteCampaigns) {
			orgPerms &^= models.PermManageCampaigns
		}
		if !models.HasAPIPermission(apiPerms, models.APIPermWriteEmails) {
			orgPerms &^= models.PermManageEmails
		}
	}
	return Actor{UserID: userID, OrgPerms: orgPerms, ViaSession: viaDashboardSession && !isKey}
}

var (
	errKeyCreateClients = errx.NewWithIdentifier(errx.Forbidden, "insufficient_api_key_permissions",
		"Creating a client workspace needs the AGENCY_MANAGE_CLIENTS permission on this key.")
	errKeyCampaigns = errx.NewWithIdentifier(errx.Forbidden, "insufficient_api_key_permissions",
		"Bringing campaigns over or linking their senders needs the WRITE_CAMPAIGNS permission on this key.")
	errKeyMailboxes = errx.NewWithIdentifier(errx.Forbidden, "insufficient_api_key_permissions",
		"Bringing mailboxes over needs the WRITE_EMAILS permission on this key.")
)

// AgencyKeyCheck refuses an agency key whose own scopes do not cover what a call
// writes; the service separately checks the member the key acts for.
func AgencyKeyCheck(apiPerms uint64, kinds []source.Kind, createsClients, linksSenders bool) *errx.Error {
	switch {
	case createsClients && !models.HasAPIPermission(apiPerms, models.APIPermAgencyManageClients):
		return errKeyCreateClients
	case (linksSenders || slices.Contains(kinds, source.KindCampaigns)) && !models.HasAPIPermission(apiPerms, models.APIPermWriteCampaigns):
		return errKeyCampaigns
	case slices.Contains(kinds, source.KindMailboxes) && !models.HasAPIPermission(apiPerms, models.APIPermWriteEmails):
		return errKeyMailboxes
	}
	return nil
}

// CreatesClients reports whether a start would create a client workspace.
func CreatesClients(in models.StartToolMigration) bool {
	for _, t := range in.Targets {
		if !t.Skip && t.CreateName != "" {
			return true
		}
	}
	return false
}
