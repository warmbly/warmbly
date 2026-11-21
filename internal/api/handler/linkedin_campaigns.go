package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// campaignLinkedInAccounts is the body and answer of /campaigns/:id/linkedin-accounts.
type campaignLinkedInAccounts struct {
	AccountIDs []uuid.UUID `json:"account_ids"`
}

// GetCampaignLinkedInAccounts is GET /v1/campaigns/:id/linkedin-accounts.
func (h *Handler) GetCampaignLinkedInAccounts(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	campaignID, ok := storeID(c)
	if !ok {
		return
	}
	ids := []uuid.UUID{}
	if h.LinkedInService != nil {
		got, err := h.LinkedInService.CampaignAccountIDs(c.Request.Context(), orgID, campaignID)
		if err != nil {
			linkedInError(c, err)
			return
		}
		ids = got
	}
	c.JSON(http.StatusOK, campaignLinkedInAccounts{AccountIDs: ids})
}

// PutCampaignLinkedInAccounts is PUT /v1/campaigns/:id/linkedin-accounts; it replaces the set, so a retry is safe.
func (h *Handler) PutCampaignLinkedInAccounts(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	campaignID, ok := storeID(c)
	if !ok {
		return
	}
	var in campaignLinkedInAccounts
	if !bindLinkedIn(c, &in) {
		return
	}
	if in.AccountIDs == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "account_ids is required; send [] to remove every LinkedIn account"))
		return
	}
	ids, err := h.LinkedInService.SetCampaignAccounts(c.Request.Context(), orgID, campaignID, in.AccountIDs)
	if err != nil {
		linkedInError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, map[string]string{
		"linkedin_accounts": strconv.Itoa(len(ids))})
	c.JSON(http.StatusOK, campaignLinkedInAccounts{AccountIDs: ids})
}

// GetLinkedInImport is GET /v1/linkedin/imports/:id.
func (h *Handler) GetLinkedInImport(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	im, err := h.LinkedInService.GetImport(c.Request.Context(), orgID, id)
	if err != nil {
		linkedInError(c, err)
		return
	}
	c.JSON(http.StatusOK, im)
}

// GetLinkedInContactActivity is GET /v1/linkedin/contacts/:contact_id/activity.
func (h *Handler) GetLinkedInContactActivity(c *gin.Context) {
	orgID, _, ok := h.linkedInCaller(c)
	if !ok {
		return
	}
	contactID, err := uuid.Parse(c.Param("contact_id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	out, aerr := h.LinkedInService.ContactActivity(c.Request.Context(), orgID, contactID)
	if aerr != nil {
		linkedInError(c, aerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// campaignDetail is a campaign with its LinkedIn accounts and how many of its leads have no LinkedIn profile.
type campaignDetail struct {
	*models.Campaign
	LinkedInAccountIDs      []uuid.UUID `json:"linkedin_account_ids"`
	LinkedInMissingURLCount *int        `json:"linkedin_missing_url_count,omitempty"`
}

// withLinkedIn adds the campaign's LinkedIn fields; a failed read leaves them out rather than failing the page.
func (h *Handler) withLinkedIn(c *gin.Context, orgID uuid.UUID, campaign *models.Campaign) any {
	if h.LinkedInService == nil || campaign == nil {
		return campaign
	}
	ids, err := h.LinkedInService.CampaignAccountIDs(c.Request.Context(), orgID, campaign.ID)
	if err != nil {
		return campaign
	}
	out := campaignDetail{Campaign: campaign, LinkedInAccountIDs: ids}
	if n, nerr := h.LinkedInService.LeadsWithoutProfile(c.Request.Context(), orgID, campaign.ID); nerr == nil {
		out.LinkedInMissingURLCount = &n
	}
	return out
}

// linkedInReadable says whether the caller may see LinkedIn counts: any member past the route's gate, or a key
// holding READ_LINKEDIN.
func linkedInReadable(c *gin.Context) bool {
	switch c.GetString(middleware.AuthTypeKey) {
	case middleware.AuthTypeAPIKey, middleware.AuthTypeOAuth:
		perms, _ := c.Get(middleware.APIKeyPermissionsKey)
		p, ok := perms.(uint64)
		return ok && models.HasAPIPermission(p, models.APIPermReadLinkedIn)
	}
	return true
}

// withLinkedInUnread adds the unread LinkedIn conversation count to a unibox overview without touching the shared one.
func (h *Handler) withLinkedInUnread(c *gin.Context, orgID uuid.UUID, o *models.UniboxOverview) *models.UniboxOverview {
	if h.LinkedInService == nil || o == nil || !linkedInReadable(c) {
		return o
	}
	n, err := h.LinkedInService.UnreadConversations(c.Request.Context(), orgID)
	if err != nil {
		return o
	}
	cp := *o
	n64 := int64(n)
	cp.LinkedInUnread = &n64
	return &cp
}
