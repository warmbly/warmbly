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

// campaignSocialAccounts is the body and answer of /campaigns/:id/social-accounts.
type campaignSocialAccounts struct {
	AccountIDs []uuid.UUID `json:"account_ids"`
}

// GetCampaignSocialAccounts is GET /v1/campaigns/:id/social-accounts.
func (h *Handler) GetCampaignSocialAccounts(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	campaignID, ok := storeID(c)
	if !ok {
		return
	}
	ids := []uuid.UUID{}
	if h.SocialService != nil {
		got, err := h.SocialService.CampaignAccountIDs(c.Request.Context(), orgID, campaignID)
		if err != nil {
			socialError(c, err)
			return
		}
		ids = got
	}
	c.JSON(http.StatusOK, campaignSocialAccounts{AccountIDs: ids})
}

// PutCampaignSocialAccounts is PUT /v1/campaigns/:id/social-accounts; it replaces the set, so a retry is safe.
func (h *Handler) PutCampaignSocialAccounts(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	campaignID, ok := storeID(c)
	if !ok {
		return
	}
	var in campaignSocialAccounts
	if !bindSocial(c, &in) {
		return
	}
	if in.AccountIDs == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "account_ids is required; send [] to remove every social account"))
		return
	}
	ids, err := h.SocialService.SetCampaignAccounts(c.Request.Context(), orgID, campaignID, in.AccountIDs)
	if err != nil {
		socialError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, map[string]string{
		"social_accounts": strconv.Itoa(len(ids))})
	c.JSON(http.StatusOK, campaignSocialAccounts{AccountIDs: ids})
}

// GetSocialImport is GET /v1/social/imports/:id.
func (h *Handler) GetSocialImport(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	im, err := h.SocialService.GetImport(c.Request.Context(), orgID, id)
	if err != nil {
		socialError(c, err)
		return
	}
	c.JSON(http.StatusOK, im)
}

// GetSocialContactActivity is GET /v1/social/contacts/:contact_id/activity.
func (h *Handler) GetSocialContactActivity(c *gin.Context) {
	orgID, _, ok := h.socialCaller(c)
	if !ok {
		return
	}
	contactID, err := uuid.Parse(c.Param("contact_id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	out, aerr := h.SocialService.ContactActivity(c.Request.Context(), orgID, contactID)
	if aerr != nil {
		socialError(c, aerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// campaignDetail is a campaign with its social senders and how many of its leads lack a handle its social steps need.
type campaignDetail struct {
	*models.Campaign
	SocialAccountIDs         []uuid.UUID `json:"social_account_ids"`
	SocialMissingHandleCount *int        `json:"social_missing_handle_count,omitempty"`
}

// withSocial adds the campaign's social fields; a failed read leaves them out rather than failing the page.
func (h *Handler) withSocial(c *gin.Context, orgID uuid.UUID, campaign *models.Campaign) any {
	if h.SocialService == nil || campaign == nil {
		return campaign
	}
	ids, err := h.SocialService.CampaignAccountIDs(c.Request.Context(), orgID, campaign.ID)
	if err != nil {
		return campaign
	}
	out := campaignDetail{Campaign: campaign, SocialAccountIDs: ids}
	if n, nerr := h.SocialService.LeadsWithoutHandle(c.Request.Context(), orgID, campaign.ID); nerr == nil {
		out.SocialMissingHandleCount = &n
	}
	return out
}

// socialReadable says whether the caller may see social counts: any member past the route's gate, or a key
// holding READ_SOCIAL.
func socialReadable(c *gin.Context) bool {
	switch c.GetString(middleware.AuthTypeKey) {
	case middleware.AuthTypeAPIKey, middleware.AuthTypeOAuth:
		perms, _ := c.Get(middleware.APIKeyPermissionsKey)
		p, ok := perms.(uint64)
		return ok && models.HasAPIPermission(p, models.APIPermReadSocial)
	}
	return true
}

// withSocialUnread adds the unread social conversation count to a unibox overview without touching the shared one.
func (h *Handler) withSocialUnread(c *gin.Context, orgID uuid.UUID, o *models.UniboxOverview) *models.UniboxOverview {
	if h.SocialService == nil || o == nil || !socialReadable(c) {
		return o
	}
	n, err := h.SocialService.UnreadConversations(c.Request.Context(), orgID)
	if err != nil {
		return o
	}
	cp := *o
	n64 := int64(n)
	cp.SocialUnread = &n64
	return &cp
}
