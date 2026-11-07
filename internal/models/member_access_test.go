package models

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The trigger in migration 000366 masks with literals; they have to be these masks.
func TestRestrictedPermissionMaskMatchesTheTrigger(t *testing.T) {
	sql, err := os.ReadFile("../infrastructure/db/migrations/000366_restricted_member_role_actions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("NEW.permissions & (%d | (%d & ", uint16(RestrictedReadPermissions), uint16(ScopeActionPermissions))
	if !strings.Contains(string(sql), want) {
		t.Fatalf("migration 000366 does not mask with %q", want)
	}
	if RestrictedPermissionMask != ScopeActionPermissions || RestrictedReadPermissions&^ScopeActionPermissions != 0 {
		t.Fatal("the read permissions must sit inside the scope action permissions")
	}
	workspaceWide := PermManageTeam | PermManageBilling | PermManageContacts | PermManageEmails | PermManageSequences |
		PermManageSettings | PermViewContacts | PermTransferOwnership | PermManageAPIKeys | PermUseIntegrations | PermUseAI
	if RestrictedPermissionMask&workspaceWide != 0 {
		t.Fatal("the restricted mask grants a workspace-wide permission")
	}
}

func TestScopeCanActOnlyWithinTheActionMask(t *testing.T) {
	s := &ResourceScope{Actions: AllPermissions}
	if !s.CanAct(PermManageCampaigns) || !s.CanAct(PermAccessUnibox) || s.CanAct(PermManageTeam) || s.CanAct(0) {
		t.Fatal("CanAct reaches outside the scope action permissions")
	}
	if (&ResourceScope{Actions: PermViewCampaigns}).CanAct(PermSendCampaigns) {
		t.Fatal("CanAct granted a permission the scope does not act with")
	}
}

func TestMemberAccessNormalize(t *testing.T) {
	id := uuid.New()
	a := &MemberAccess{Scope: AccessScopeWorkspace, CampaignIDs: []uuid.UUID{id}}
	a.Normalize()
	if len(a.CampaignIDs) != 0 || a.CampaignIDs == nil {
		t.Fatalf("a workspace scope kept grants: %+v", a)
	}
	r := &MemberAccess{Scope: AccessScopeRestricted, CampaignIDs: []uuid.UUID{id, id, uuid.Nil}}
	r.Normalize()
	if len(r.CampaignIDs) != 1 || r.FolderIDs == nil || r.EmailAccountIDs == nil {
		t.Fatalf("restricted normalize = %+v", r)
	}
	owner := &OrganizationMember{Role: string(RoleOwner), AccessScope: AccessScopeRestricted}
	if owner.IsRestricted() {
		t.Fatal("the owner read as restricted")
	}
}
