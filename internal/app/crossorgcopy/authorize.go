// Package crossorgcopy copies a campaign or reply templates from one
// workspace into others the caller also belongs to, as fresh drafts.
package crossorgcopy

import (
	"slices"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Stable codes this package answers with.
const (
	CodeRequiresSession      = "cross_workspace_copy_requires_session"
	CodeDestinationForbidden = "copy_destination_forbidden"
	CodeTooManyDestinations  = "copy_destinations_limit"
	CodeTooManyTemplates     = "copy_templates_limit"

	// codeMemberAccessRestricted is middleware.MemberAccessRestricted.
	codeMemberAccessRestricted = "member_access_restricted"
)

// Caller is who asks for a copy, as the route resolved them.
type Caller struct {
	UserID      uuid.UUID
	SourceOrgID uuid.UUID
	// KeyBound is an API key or OAuth token: it reaches only SourceOrgID.
	KeyBound bool
	// AllowedCampaigns is the restricted member's campaign grant; nil is every campaign.
	AllowedCampaigns []uuid.UUID
}

// destination is one workspace a copy may be written to, or why it may not.
type destination struct {
	orgID  uuid.UUID
	name   string
	refuse *models.CopyFailure
}

var errDestinationForbidden = models.CopyFailure{
	Code:    CodeDestinationForbidden,
	Message: "You need permission to manage campaigns in this workspace, with access to the whole workspace, to copy into it.",
}

// authorize decides, from the caller's own memberships, whether the source
// may be read and which destinations may be written. A destination is
// writable only for an accepted, unrestricted member holding manage_campaigns.
// The returned destinations keep the request's order, without repeats.
func authorize(caller Caller, members []models.OrganizationMember, requested []uuid.UUID, campaignID *uuid.UUID, withTemplates bool) ([]destination, *errx.Error) {
	dests := dedupe(requested)
	if len(dests) == 0 {
		return nil, errx.New(errx.BadRequest, "organization_ids must name at least one workspace")
	}
	if len(dests) > models.CopyToWorkspacesMax {
		return nil, errx.NewWithIdentifier(errx.BadRequest, CodeTooManyDestinations,
			"A copy can go to at most 10 workspaces at a time.")
	}
	if caller.KeyBound {
		for _, d := range dests {
			if d != caller.SourceOrgID {
				return nil, errx.NewWithIdentifier(errx.Forbidden, CodeRequiresSession,
					"API keys and OAuth tokens reach one workspace. Copy to another workspace from the dashboard, signed in as a member of both.")
			}
		}
	}

	byOrg := make(map[uuid.UUID]*models.OrganizationMember, len(members))
	for i := range members {
		byOrg[members[i].OrganizationID] = &members[i]
	}

	source := byOrg[caller.SourceOrgID]
	if source == nil || source.AcceptedAt == nil || !source.HasPermission(models.PermViewCampaigns) {
		return nil, errx.ErrForbidden
	}
	// A restricted member reads only what was shared with them; copying it elsewhere would take it out of the workspace.
	if source.IsRestricted() {
		return nil, errx.NewWithIdentifier(errx.Forbidden, codeMemberAccessRestricted,
			"Your access to this workspace is limited to selected campaigns and mailboxes, so nothing in it can be copied to another workspace.")
	}
	if campaignID != nil && caller.AllowedCampaigns != nil && !grants(caller.AllowedCampaigns, *campaignID) {
		return nil, errx.New(errx.NotFound, "campaign not found")
	}

	out := make([]destination, 0, len(dests))
	for _, id := range dests {
		d := destination{orgID: id}
		m := byOrg[id]
		if m != nil && m.Organization != nil {
			d.name = m.Organization.Name
		}
		if m == nil || m.AcceptedAt == nil || m.IsRestricted() || !m.HasPermission(models.PermManageCampaigns) {
			refusal := errDestinationForbidden
			d.refuse = &refusal
		}
		out = append(out, d)
	}
	return out, nil
}

func grants(allowed []uuid.UUID, id uuid.UUID) bool {
	return id != uuid.Nil && slices.Contains(allowed, id)
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
