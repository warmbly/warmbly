package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ResourceScopeKey holds a restricted caller's resolved *models.ResourceScope.
const ResourceScopeKey = "resource_scope"

// MemberAccessRestricted is the code for a restricted member on a surface their scope does not cover.
const MemberAccessRestricted = "member_access_restricted"

var errMemberAccessRestricted = errx.NewWithIdentifier(errx.Forbidden, MemberAccessRestricted,
	"Your access to this workspace is limited to selected campaigns and mailboxes, and this is outside it.")

// These services authorize the account or target workspace, not the currently selected membership.
var organizationPolicyRoutes = map[string]bool{
	"POST /v1/organization":                       true,
	"POST /v1/organization/:orgId/limit-requests": true,
	"GET /v1/organization/:orgId/limit-requests":  true,
	"DELETE /v1/limit-requests/:id":               true,
}

// scopeAwareRoutes is every route a restricted member may call; each one filters to the member's grants.
var scopeAwareRoutes = map[string]bool{
	// The member's own account, workspace list and socket.
	"GET /v1/me":                             true,
	"GET /v1/organization":                   true,
	"POST /v1/organization/switch/:id":       true,
	"GET /v1/organization/current":           true,
	"GET /v1/me/views/:view":                 true,
	"PUT /v1/me/views/:view":                 true,
	"DELETE /v1/me/views/:view":              true,
	"GET /v1/me/danger-zone":                 true,
	"POST /v1/me/danger-zone/delete":         true,
	"DELETE /v1/me/danger-zone/delete":       true,
	"GET /v1/invitations":                    true,
	"POST /v1/invitations/accept":            true,
	"POST /v1/getaway":                       true,
	"GET /v1/subscription":                   true,
	"GET /v1/subscription/features":          true,
	"GET /v1/campaigns":                      true,
	"GET /v1/campaigns-overview":             true,
	"GET /v1/campaigns/:id":                  true,
	"GET /v1/campaigns/:id/logs":             true,
	"GET /v1/campaigns/:id/ab-variants":      true,
	"GET /v1/timezones":                      true,
	"GET /v1/campaigns/:id/steps":            true,
	"GET /v1/analytics/dashboard":            true,
	"GET /v1/analytics/campaigns/compare":    true,
	"GET /v1/analytics/campaigns/:id":        true,
	"GET /v1/analytics/campaigns/:id/daily":  true,
	"GET /v1/analytics/campaigns/:id/hourly": true,
	"GET /v1/analytics/accounts":             true,
	"GET /v1/analytics/accounts/:id":         true,
	"GET /v1/emails":                         true,
	"GET /v1/emails/:id":                     true,
	"GET /v1/unibox":                         true,
	"GET /v1/unibox/count":                   true,
	"GET /v1/unibox/overview":                true,
	"GET /v1/unibox/thread":                  true,
	"GET /v1/unibox/thread/labels":           true,
	"GET /v1/unibox/scheduled":               true,
	"GET /v1/unibox/:id":                     true,
}

// ScopeCheck names how a scope action route holds its target to the member's grants.
type ScopeCheck string

const (
	// ScopeCheckCampaignParam is RequireCampaignParam("id") on the campaigns group.
	ScopeCheckCampaignParam ScopeCheck = "campaign_param"
	// ScopeCheckMailbox is the handler refusing any message, thread or sender outside AllowedEmailAccounts.
	ScopeCheckMailbox ScopeCheck = "mailbox"
	// ScopeCheckOwnRows is a write to the caller's own rows only.
	ScopeCheckOwnRows ScopeCheck = "own_rows"
)

// ScopeAction is what a scope action route needs: a role acting in scope with Perm, and the grant check it runs.
type ScopeAction struct {
	Perm  models.OrganizationPermission
	Check ScopeCheck
}

// scopeActionRoutes are the writes a restricted member may make through a role that acts in scope.
var scopeActionRoutes = map[string]ScopeAction{
	"PUT /v1/unibox/thread/labels":         {models.PermAccessUnibox, ScopeCheckMailbox},
	"PATCH /v1/unibox/seen":                {models.PermAccessUnibox, ScopeCheckMailbox},
	"PATCH /v1/unibox/folder":              {models.PermAccessUnibox, ScopeCheckMailbox},
	"POST /v1/unibox/reply":                {models.PermAccessUnibox, ScopeCheckMailbox},
	"POST /v1/unibox/snooze":               {models.PermAccessUnibox, ScopeCheckMailbox},
	"DELETE /v1/unibox/snooze":             {models.PermAccessUnibox, ScopeCheckOwnRows},
	"DELETE /v1/unibox/scheduled/:task_id": {models.PermAccessUnibox, ScopeCheckMailbox},
	"PATCH /v1/campaigns/:id":              {models.PermManageCampaigns, ScopeCheckCampaignParam},
	"POST /v1/campaigns/:id/steps":         {models.PermManageCampaigns, ScopeCheckCampaignParam},
	"PATCH /v1/campaigns/:id/steps/:sid":   {models.PermManageCampaigns, ScopeCheckCampaignParam},
	"DELETE /v1/campaigns/:id/steps/:sid":  {models.PermManageCampaigns, ScopeCheckCampaignParam},
	"PATCH /v1/campaigns/:id/step-layout":  {models.PermManageCampaigns, ScopeCheckCampaignParam},
	"POST /v1/campaigns/:id/start":         {models.PermSendCampaigns, ScopeCheckCampaignParam},
	"POST /v1/campaigns/:id/stop":          {models.PermSendCampaigns, ScopeCheckCampaignParam},
}

// ScopeAwareRoutes lists grant-aware and target-authorized routes for the route-table test.
func ScopeAwareRoutes() []string {
	out := make([]string, 0, len(scopeAwareRoutes)+len(organizationPolicyRoutes)+len(scopeActionRoutes))
	for r := range scopeAwareRoutes {
		out = append(out, r)
	}
	for r := range organizationPolicyRoutes {
		out = append(out, r)
	}
	for r := range scopeActionRoutes {
		out = append(out, r)
	}
	return out
}

// ScopeActionRoutes is scopeActionRoutes for the route-table test.
func ScopeActionRoutes() map[string]ScopeAction {
	out := make(map[string]ScopeAction, len(scopeActionRoutes))
	for r, a := range scopeActionRoutes {
		out[r] = a
	}
	return out
}

// errScopeActionRefused is a restricted member writing without a role that acts in scope.
var errScopeActionRefused = errx.NewWithIdentifier(errx.Forbidden, MemberAccessRestricted,
	"Your roles let you view the campaigns and mailboxes shared with you, not change them.")

// ResourceScopeGate holds a restricted member, and any key or app acting for one, to scope-aware routes and their grants.
func (h *Handler) ResourceScopeGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if organizationPolicyRoutes[c.Request.Method+" "+c.FullPath()] {
			c.Next()
			return
		}
		m := GetAuthMember(c)
		if !m.IsRestricted() {
			c.Next()
			return
		}
		route := c.Request.Method + " " + c.FullPath()
		action, acts := scopeActionRoutes[route]
		if !acts && !scopeAwareRoutes[route] {
			errx.JSON(c, errMemberAccessRestricted)
			c.Abort()
			return
		}
		if h.OrganizationService == nil {
			errx.JSON(c, errx.InternalError())
			c.Abort()
			return
		}
		scope, xerr := h.OrganizationService.ResolveMemberScope(c.Request.Context(), m.OrganizationID, m.UserID)
		if xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}
		if scope == nil {
			errx.JSON(c, errx.InternalError())
			c.Abort()
			return
		}
		if acts && !scope.CanAct(action.Perm) {
			errx.JSON(c, errScopeActionRefused)
			c.Abort()
			return
		}
		c.Set(ResourceScopeKey, scope)
		c.Next()
	}
}

// grantList is a restricted member's grants as an allowlist, which is never empty.
func grantList(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return models.NoneMatch()
	}
	return ids
}

// GetResourceScope is the caller's resolved scope, or nil when they reach the whole workspace.
func GetResourceScope(c *gin.Context) *models.ResourceScope {
	if v, ok := c.Get(ResourceScopeKey); ok {
		if s, ok := v.(*models.ResourceScope); ok {
			return s
		}
	}
	return nil
}

// ScopeCanAct reports whether a restricted caller may act with perm inside their grants; unrestricted callers report false.
func ScopeCanAct(c *gin.Context, perm models.OrganizationPermission) bool {
	s := GetResourceScope(c)
	return s != nil && s.CanAct(perm)
}

// IsScopeRestricted reports whether the caller acts for a restricted member.
func IsScopeRestricted(c *gin.Context) bool {
	return GetResourceScope(c) != nil || GetAuthMember(c).IsRestricted()
}

// AllowedCampaigns is the caller's campaign allowlist: nil is every campaign, and an empty grant matches none.
func AllowedCampaigns(c *gin.Context) []uuid.UUID {
	if s := GetResourceScope(c); s != nil {
		return grantList(s.Campaigns)
	}
	if GetAuthMember(c).IsRestricted() {
		return models.NoneMatch()
	}
	return nil
}

// CampaignAllowed reports whether the caller may read campaign id.
func CampaignAllowed(c *gin.Context, id uuid.UUID) bool {
	allowed := AllowedCampaigns(c)
	if allowed == nil {
		return true
	}
	for _, v := range allowed {
		if v == id && v != uuid.Nil {
			return true
		}
	}
	return false
}

// AllowedFolders are the campaign folders the caller may see, nil for every folder.
func AllowedFolders(c *gin.Context) []uuid.UUID {
	if s := GetResourceScope(c); s != nil {
		return grantList(s.Folders)
	}
	if GetAuthMember(c).IsRestricted() {
		return models.NoneMatch()
	}
	return nil
}

// AllowedEmailAccounts is the caller's mailbox allowlist (an API key's list and a restricted member's grants together); nil is every mailbox.
func AllowedEmailAccounts(c *gin.Context) []uuid.UUID {
	var key []uuid.UUID
	if c.GetString(AuthTypeKey) == AuthTypeAPIKey {
		key = GetAPIKeyAllowedEmailAccounts(c)
	}
	var member []uuid.UUID
	if s := GetResourceScope(c); s != nil {
		member = grantList(s.Mailboxes)
	} else if GetAuthMember(c).IsRestricted() {
		member = models.NoneMatch()
	}
	switch {
	case len(key) == 0:
		return member
	case member == nil:
		return key
	}
	both := make([]uuid.UUID, 0, len(key))
	for _, id := range key {
		for _, m := range member {
			if id == m {
				both = append(both, id)
				break
			}
		}
	}
	return grantList(both)
}

// EmailAccountAllowed reports whether the caller may act on mailbox id.
func EmailAccountAllowed(c *gin.Context, id uuid.UUID) bool {
	allowed := AllowedEmailAccounts(c)
	if allowed == nil {
		return true
	}
	for _, v := range allowed {
		if v == id && v != uuid.Nil {
			return true
		}
	}
	return false
}

// ErrEmailAccountNotAllowed is the refusal for a mailbox outside the caller's allowlist.
func ErrEmailAccountNotAllowed(c *gin.Context) *errx.Error {
	if IsScopeRestricted(c) {
		return errx.New(errx.NotFound, "email account not found")
	}
	return errx.New(errx.Forbidden, "email account is not allowed for this API key")
}

// RequireEmailAccountParam enforces the caller's mailbox allowlist on a route parameter.
func RequireEmailAccountParam(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if AllowedEmailAccounts(c) == nil {
			c.Next()
			return
		}
		accountID, err := uuid.Parse(c.Param(param))
		if err != nil {
			errx.Handle(c, errx.ErrUuid)
			c.Abort()
			return
		}
		if EmailAccountAllowed(c, accountID) {
			c.Next()
			return
		}
		errx.Handle(c, ErrEmailAccountNotAllowed(c))
		c.Abort()
	}
}

// RequireCampaignParam answers not found for a campaign outside a restricted caller's scope; routes without the param pass.
func RequireCampaignParam(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if AllowedCampaigns(c) == nil || c.Param(param) == "" {
			c.Next()
			return
		}
		id, err := uuid.Parse(c.Param(param))
		if err != nil {
			errx.Handle(c, errx.ErrUuid)
			c.Abort()
			return
		}
		if CampaignAllowed(c, id) {
			c.Next()
			return
		}
		errx.Handle(c, errx.New(errx.NotFound, "campaign not found"))
		c.Abort()
	}
}
