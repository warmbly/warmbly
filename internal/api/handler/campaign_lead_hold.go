package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// pauseLeadRequest parks one lead's flow. `until` absent or null holds it with
// no end, which only a resume lifts.
type pauseLeadRequest struct {
	Until  *time.Time `json:"until"`
	Reason string     `json:"reason"`
}

// leadHoldResponse is the shape all three endpoints answer with. `hold` is
// absent when the lead is not held.
type leadHoldResponse struct {
	CampaignID string           `json:"campaign_id"`
	ContactID  string           `json:"contact_id"`
	Hold       *models.LeadHold `json:"hold,omitempty"`
}

func leadHoldOK(c *gin.Context, campaignID, contactID uuid.UUID, hold *models.LeadHold) {
	c.JSON(http.StatusOK, leadHoldResponse{
		CampaignID: campaignID.String(), ContactID: contactID.String(), Hold: hold,
	})
}

// GetCampaignLeadHold reads whether one lead's flow is currently held.
//
// GET /campaigns/:id/leads/:contactId/hold
func (h *Handler) GetCampaignLeadHold(c *gin.Context) {
	orgID, campaignID, contactID, ok := leadHoldParams(c)
	if !ok {
		return
	}
	hold, xerr := h.CampaignService.GetLeadHold(c.Request.Context(), orgID, campaignID, contactID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	leadHoldOK(c, campaignID, contactID, hold)
}

// PauseCampaignLead parks one contact's flow inside one campaign until a date,
// or with no end. The contact stays subscribed and stays a lead of the
// campaign: this is not an unsubscribe and not a suppression.
//
// No Idempotency-Key: the request states an absolute hold, and replacing a
// live hold keeps its start, so a retry lands on exactly the same row.
//
// POST /campaigns/:id/leads/:contactId/pause
func (h *Handler) PauseCampaignLead(c *gin.Context) {
	orgID, campaignID, contactID, ok := leadHoldParams(c)
	if !ok {
		return
	}
	var req pauseLeadRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
			return
		}
	}
	hold, xerr := h.CampaignService.PauseLead(c.Request.Context(), orgID, campaignID, contactID, req.Until, req.Reason)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionPause, models.AuditEntityCampaignLead, &contactID, nil, map[string]string{
		"campaign_id": campaignID.String(),
		"until":       holdUntilLabel(hold),
	})
	leadHoldOK(c, campaignID, contactID, hold)
}

// ResumeCampaignLead lifts the hold now. Resuming a lead that is not held is a
// success, not a 404: the caller asked for "not held" and that is the state.
// A contact that is not a lead of the campaign is still a 404, as it is for
// pause.
//
// POST /campaigns/:id/leads/:contactId/resume
func (h *Handler) ResumeCampaignLead(c *gin.Context) {
	orgID, campaignID, contactID, ok := leadHoldParams(c)
	if !ok {
		return
	}
	if xerr := h.CampaignService.ResumeLead(c.Request.Context(), orgID, campaignID, contactID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionResume, models.AuditEntityCampaignLead, &contactID, nil, map[string]string{
		"campaign_id": campaignID.String(),
	})
	leadHoldOK(c, campaignID, contactID, nil)
}

// leadHoldParams resolves the organization and both path ids through the same
// helpers every other handler uses, so a missing org or a malformed id answers
// the way the rest of the API does.
func leadHoldParams(c *gin.Context) (orgID, campaignID, contactID uuid.UUID, ok bool) {
	if orgID, ok = requireOrgID(c); !ok {
		return
	}
	if campaignID, ok = parseUUIDParam(c, "id"); !ok {
		return
	}
	contactID, ok = parseUUIDParam(c, "contactId")
	return
}

// holdUntilLabel is the audit trail's word for when a hold lifts.
func holdUntilLabel(hold *models.LeadHold) string {
	if hold == nil || hold.Until == nil {
		return "no end"
	}
	return hold.Until.UTC().Format(time.RFC3339)
}
