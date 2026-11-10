package crossorgcopy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func member(org uuid.UUID, role string, perms models.OrganizationPermission, scope models.AccessScope) models.OrganizationMember {
	now := time.Now()
	return models.OrganizationMember{
		OrganizationID: org,
		Role:           role,
		Permissions:    perms,
		AccessScope:    scope,
		AcceptedAt:     &now,
		Organization:   &models.Organization{ID: org, Name: "ws-" + org.String()[:4]},
	}
}

func TestAuthorizeMatrix(t *testing.T) {
	src, dst := uuid.New(), uuid.New()
	campaign := uuid.New()
	viewer := models.PermViewCampaigns
	manager := models.PermViewCampaigns | models.PermManageCampaigns
	ws := models.AccessScopeWorkspace
	restricted := models.AccessScopeRestricted
	pending := member(dst, "member", manager, ws)
	pending.AcceptedAt = nil

	cases := []struct {
		name      string
		caller    Caller
		members   []models.OrganizationMember
		templates bool
		wantCode  string // whole-request refusal
		wantDest  string // "" allowed, else the per-destination code
	}{
		{"manager in both", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "member", viewer, ws), member(dst, "member", manager, ws)}, true, "", ""},
		{"owner bypasses mask", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "owner", 0, ws), member(dst, "owner", 0, ws)}, true, "", ""},
		{"no source membership", Caller{SourceOrgID: src}, []models.OrganizationMember{member(dst, "member", manager, ws)}, false, "forbidden", ""},
		{"source without view", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "member", models.PermManageEmails, ws), member(dst, "member", manager, ws)}, false, "forbidden", ""},
		{"not a member of destination", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "member", viewer, ws)}, false, "", CodeDestinationForbidden},
		{"destination viewer only", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "member", viewer, ws), member(dst, "member", viewer, ws)}, false, "", CodeDestinationForbidden},
		{"destination invitation not accepted", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "member", viewer, ws), pending}, false, "", CodeDestinationForbidden},
		{"restricted in destination", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "member", viewer, ws), member(dst, "member", manager, restricted)}, false, "", CodeDestinationForbidden},
		{"restricted source, granted campaign", Caller{SourceOrgID: src, AllowedCampaigns: []uuid.UUID{campaign}}, []models.OrganizationMember{member(src, "member", viewer, restricted), member(dst, "member", manager, ws)}, false, codeMemberAccessRestricted, ""},
		{"restricted source, campaign not granted", Caller{SourceOrgID: src, AllowedCampaigns: []uuid.UUID{uuid.New()}}, []models.OrganizationMember{member(src, "member", viewer, restricted), member(dst, "member", manager, ws)}, false, codeMemberAccessRestricted, ""},
		{"restricted source, no grants", Caller{SourceOrgID: src}, []models.OrganizationMember{member(src, "member", viewer, restricted), member(dst, "member", manager, ws)}, false, codeMemberAccessRestricted, ""},
		{"restricted source cannot carry templates", Caller{SourceOrgID: src, AllowedCampaigns: []uuid.UUID{campaign}}, []models.OrganizationMember{member(src, "member", viewer, restricted), member(dst, "member", manager, ws)}, true, codeMemberAccessRestricted, ""},
		{"api key to another workspace", Caller{SourceOrgID: src, KeyBound: true}, []models.OrganizationMember{member(src, "member", manager, ws), member(dst, "member", manager, ws)}, false, CodeRequiresSession, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dests, xerr := authorize(tc.caller, tc.members, []uuid.UUID{dst}, &campaign, tc.templates)
			if tc.wantCode != "" {
				if xerr == nil {
					t.Fatalf("want refusal %q, got %d destinations", tc.wantCode, len(dests))
				}
				if got := xerr.ResponseCode(); got != tc.wantCode {
					t.Fatalf("code = %q, want %q", got, tc.wantCode)
				}
				return
			}
			if xerr != nil {
				t.Fatalf("unexpected refusal: %v", xerr)
			}
			if len(dests) != 1 || dests[0].orgID != dst {
				t.Fatalf("destinations = %+v", dests)
			}
			got := ""
			if dests[0].refuse != nil {
				got = dests[0].refuse.Code
			}
			if got != tc.wantDest {
				t.Fatalf("destination code = %q, want %q", got, tc.wantDest)
			}
		})
	}
}

func TestAuthorizeKeyMayCopyWithinItsWorkspace(t *testing.T) {
	src := uuid.New()
	members := []models.OrganizationMember{member(src, "member", models.PermViewCampaigns|models.PermManageCampaigns, models.AccessScopeWorkspace)}
	dests, xerr := authorize(Caller{SourceOrgID: src, KeyBound: true}, members, []uuid.UUID{src}, nil, true)
	if xerr != nil || len(dests) != 1 || dests[0].refuse != nil {
		t.Fatalf("dests=%+v err=%v", dests, xerr)
	}
}

func TestAuthorizeBoundsAndDedupesDestinations(t *testing.T) {
	src := uuid.New()
	members := []models.OrganizationMember{member(src, "owner", 0, models.AccessScopeWorkspace)}
	if _, xerr := authorize(Caller{SourceOrgID: src}, members, nil, nil, false); xerr == nil || xerr.Code != errx.BadRequest {
		t.Fatalf("empty destinations: %v", xerr)
	}
	many := make([]uuid.UUID, models.CopyToWorkspacesMax+1)
	for i := range many {
		many[i] = uuid.New()
	}
	if _, xerr := authorize(Caller{SourceOrgID: src}, members, many, nil, false); xerr == nil || xerr.ResponseCode() != CodeTooManyDestinations {
		t.Fatalf("over the cap: %v", xerr)
	}
	dst := uuid.New()
	dests, xerr := authorize(Caller{SourceOrgID: src}, members, []uuid.UUID{dst, dst, uuid.Nil, dst}, nil, false)
	if xerr != nil || len(dests) != 1 {
		t.Fatalf("dedupe: %+v %v", dests, xerr)
	}
}
