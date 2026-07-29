package aitools

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Team / membership tools. These HTTP routes are JWT-session only (no API-key
// permission bit), so every tool here is JWTOnly: the dashboard agent can use
// them, but developer API keys / MCP callers cannot reach org governance.
func (d Deps) registerTeamTools(r *Registry) {
	if d.Org == nil {
		return
	}

	r.Register(Tool{
		Name:            "list_team_members",
		Description:     "List the organization's members with their roles.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: 0,
		Handler:         d.listTeamMembers,
	})

	r.Register(Tool{
		Name:        "invite_member",
		Description: "Invite a user to the organization by email with one or more roles. Returns the invitation id.",
		InputSchema: objectSchema(map[string]any{
			"email":    strProp("The invitee's email address."),
			"role_id":  strProp("A single role UUID to assign."),
			"role_ids": arrProp("Role UUIDs to assign (use instead of role_id for multiple).", strProp("Role UUID.")),
		}, "email"),
		Risk:            generation.RiskWrite,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageTeam,
		Handler:         d.inviteMember,
	})

	r.Register(Tool{
		Name:        "update_member_role",
		Description: "Change a member's role assignment.",
		InputSchema: objectSchema(map[string]any{
			"member_user_id": strProp("The member's user UUID."),
			"role_id":        strProp("A single role UUID to set."),
			"role_ids":       arrProp("Role UUIDs to set (replaces the member's roles).", strProp("Role UUID.")),
		}, "member_user_id"),
		Risk:            generation.RiskWrite,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageTeam,
		Handler:         d.updateMemberRole,
	})

	r.Register(Tool{
		Name:        "remove_member",
		Description: "Remove a member from the organization. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"member_user_id": strProp("The member's user UUID."),
		}, "member_user_id"),
		Risk:            generation.RiskWrite,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageTeam,
		Handler:         d.removeMember,
	})

	r.Register(Tool{
		Name:            "list_invitations",
		Description:     "List the organization's pending invitations.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageTeam,
		Handler:         d.listInvitations,
	})

	r.Register(Tool{
		Name:        "cancel_invitation",
		Description: "Cancel a pending invitation.",
		InputSchema: objectSchema(map[string]any{
			"invitation_id": strProp("The invitation UUID."),
		}, "invitation_id"),
		Risk:            generation.RiskWrite,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageTeam,
		Handler:         d.cancelInvitation,
	})

	r.Register(Tool{
		Name:        "get_invitation_link",
		Description: "Get the shareable invite link/token for a pending invitation.",
		InputSchema: objectSchema(map[string]any{
			"invitation_id": strProp("The invitation UUID."),
		}, "invitation_id"),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageTeam,
		Handler:         d.getInvitationLink,
	})
}

func (d Deps) listTeamMembers(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	members, xerr := d.Org.GetMembers(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"members": members, "count": len(members)})
}

// roleIDsFromArgs collects role UUIDs from a single role_id and/or a role_ids
// list, parsing each.
func roleIDsFromArgs(roleID string, roleIDs []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(roleIDs)+1)
	if roleID != "" {
		id, err := parseUUIDArg(roleID)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	for _, r := range roleIDs {
		id, err := parseUUIDArg(r)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (d Deps) inviteMember(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Email   string   `json:"email"`
		RoleID  string   `json:"role_id"`
		RoleIDs []string `json:"role_ids"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Email == "" {
		return "", ErrInvalidArgs
	}
	roles, err := roleIDsFromArgs(in.RoleID, in.RoleIDs)
	if err != nil {
		return "", err
	}
	req := &models.InviteMemberRequest{Email: in.Email, RoleIDs: roles}
	invite, xerr := d.Org.InviteMember(ctx, inv.OrgID, inv.UserID, req)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionInvite, models.AuditEntityInvitation, &invite.ID, map[string]string{"email": in.Email})
	return jsonResult(map[string]any{"ok": true, "invitation_id": invite.ID.String()})
}

func (d Deps) updateMemberRole(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		MemberUserID string   `json:"member_user_id"`
		RoleID       string   `json:"role_id"`
		RoleIDs      []string `json:"role_ids"`
	}](args)
	if err != nil {
		return "", err
	}
	memberID, err := parseUUIDArg(in.MemberUserID)
	if err != nil {
		return "", err
	}
	roles, err := roleIDsFromArgs(in.RoleID, in.RoleIDs)
	if err != nil {
		return "", err
	}
	if len(roles) == 0 {
		return "", ErrInvalidArgs
	}
	member, xerr := d.Org.UpdateMemberRole(ctx, inv.OrgID, inv.UserID, memberID, &models.UpdateMemberRequest{RoleIDs: roles})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityOrganizationMember, &memberID, nil)
	return jsonResult(member)
}

func (d Deps) removeMember(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		MemberUserID string `json:"member_user_id"`
	}](args)
	if err != nil {
		return "", err
	}
	memberID, err := parseUUIDArg(in.MemberUserID)
	if err != nil {
		return "", err
	}
	if xerr := d.Org.RemoveMember(ctx, inv.OrgID, memberID); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionRemove, models.AuditEntityOrganizationMember, &memberID, nil)
	return jsonResult(map[string]any{"ok": true, "member_user_id": memberID.String()})
}

func (d Deps) listInvitations(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	invites, xerr := d.Org.GetPendingInvitations(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"invitations": invites, "count": len(invites)})
}

func (d Deps) cancelInvitation(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		InvitationID string `json:"invitation_id"`
	}](args)
	if err != nil {
		return "", err
	}
	iid, err := parseUUIDArg(in.InvitationID)
	if err != nil {
		return "", err
	}
	if xerr := d.Org.CancelInvitation(ctx, iid); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionRemove, models.AuditEntityInvitation, &iid, nil)
	return jsonResult(map[string]any{"ok": true, "invitation_id": iid.String()})
}

func (d Deps) getInvitationLink(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		InvitationID string `json:"invitation_id"`
	}](args)
	if err != nil {
		return "", err
	}
	iid, err := parseUUIDArg(in.InvitationID)
	if err != nil {
		return "", err
	}
	token, xerr := d.Org.GetInvitationToken(ctx, inv.OrgID, iid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"invitation_id": iid.String(), "token": token})
}
