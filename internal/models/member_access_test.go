package models

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The trigger in migration 000278 masks with a literal; it has to be this mask.
func TestRestrictedPermissionMaskMatchesTheTrigger(t *testing.T) {
	sql, err := os.ReadFile("../infrastructure/db/migrations/000278_member_access_scope.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("NEW.permissions & %d;", uint16(RestrictedPermissionMask))
	if !strings.Contains(string(sql), want) {
		t.Fatalf("migration 000278 does not mask with %q", want)
	}
	if RestrictedPermissionMask&(PermManageCampaigns|PermManageEmails|PermManageTeam|PermViewContacts|PermUseAI) != 0 {
		t.Fatal("the restricted mask grants more than reading campaigns, analytics and the inbox")
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
