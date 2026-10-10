package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

type audienceMembers struct {
	members []models.OrganizationMember
	err     error
}

func (m *audienceMembers) GetMembers(context.Context, uuid.UUID) ([]models.OrganizationMember, error) {
	return m.members, m.err
}

func acceptedMember(userID uuid.UUID) *audienceMembers {
	now := time.Now()
	return &audienceMembers{members: []models.OrganizationMember{{UserID: userID, AcceptedAt: &now, AccessScope: models.AccessScopeWorkspace}}}
}

func TestWorkspaceNotificationIngress(t *testing.T) {
	userID, orgID := uuid.New(), uuid.New()
	for _, state := range []string{"unrestricted", "restricted", "removed", "invited", "error", "missing", "personal", "unscoped workspace"} {
		t.Run(state, func(t *testing.T) {
			members := acceptedMember(userID)
			var lookup MemberLookup = members
			org, category := &orgID, models.NotifCampaignPaused
			want := false
			switch state {
			case "unrestricted":
				want = true
			case "restricted":
				members.members[0].AccessScope = models.AccessScopeRestricted
			case "removed":
				members.members = nil
			case "invited":
				members.members[0].AcceptedAt = nil
			case "error":
				members.err = errors.New("resolver unavailable")
			case "missing":
				lookup = nil
			case "personal":
				org, category, lookup, want = nil, models.NotifSecuritySignIn, nil, true
			case "unscoped workspace":
				org = nil
			}
			repo := &messageNotificationRepo{channels: models.ChannelPrefs{InApp: true, Email: true}}
			s := &service{repo: repo, members: lookup}
			created, err := s.notifyOneWithError(context.Background(), userID, org, nil, category, "private", "private", "", nil, "", false)
			if (repo.created > 0) != want {
				t.Fatalf("created=%v stored=%d want=%v err=%v", created, repo.created, want, err)
			}
			if (state == "error" || state == "missing") && err == nil {
				t.Fatal("resolver failure must remain retryable")
			}
		})
	}
}

func TestDeferredNotificationRevocation(t *testing.T) {
	ctx := context.Background()
	userID, orgID := uuid.New(), uuid.New()
	members := acceptedMember(userID)
	repo := &messageNotificationRepo{}
	s := &service{repo: repo, members: members}
	workspace := models.Notification{ID: uuid.New(), UserID: userID, OrganizationID: &orgID, Category: models.NotifCampaignPaused}
	personal := models.Notification{ID: uuid.New(), UserID: userID, Category: models.NotifSecuritySignIn}
	push := pendingPush{OrganizationID: &orgID, Title: "private"}
	if !s.canDeliverPush(ctx, userID, workspace.Category, push) {
		t.Fatal("unrestricted push must remain eligible")
	}
	for _, state := range []string{"restricted", "removed", "error", "missing"} {
		t.Run(state, func(t *testing.T) {
			members = acceptedMember(userID)
			s.members = members
			switch state {
			case "restricted":
				members.members[0].AccessScope = models.AccessScopeRestricted
			case "removed":
				members.members = nil
			case "error":
				members.err = errors.New("unavailable")
			case "missing":
				s.members = nil
			}
			kept := s.keepEligibleEmailMessages(ctx, []models.Notification{workspace, personal})
			if len(kept) != 1 || kept[0].ID != personal.ID {
				t.Fatalf("workspace email survived revocation: %+v", kept)
			}
			if s.canDeliverPush(ctx, userID, workspace.Category, push) {
				t.Fatal("workspace push survived revocation")
			}
			if !s.canDeliverPush(ctx, userID, personal.Category, pendingPush{}) {
				t.Fatal("personal push requires no workspace")
			}
		})
	}
	if s.canDeliverPush(ctx, userID, workspace.Category, pendingPush{Title: "legacy batch"}) {
		t.Fatal("legacy queued workspace push without provenance must fail closed")
	}
}
