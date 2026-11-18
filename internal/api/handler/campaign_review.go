package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/leadreview"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) reviewReady(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.LeadReview == nil {
		errx.JSON(c, errx.InternalError())
		return uuid.Nil, uuid.Nil, false
	}
	orgID, ok := requireOrgID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	campaignID, ok := parseUUIDParam(c, "id")
	return orgID, campaignID, ok
}

// GetCampaignReview answers the Review tab's readiness report.
//
// GET /campaigns/:id/review
func (h *Handler) GetCampaignReview(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	out, xerr := h.LeadReview.Summary(c.Request.Context(), orgID, campaignID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

type setReviewRequest struct {
	Enabled *bool `json:"enabled"`
}

// SetCampaignReview turns review mode on or off. Turning it off approves every
// waiting lead, which puts mail in motion, so it takes the send permission.
// It states an absolute mode, so a retry changes nothing more.
//
// PUT /campaigns/:id/review
func (h *Handler) SetCampaignReview(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	var req setReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if req.Enabled == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "enabled is required"))
		return
	}
	if !*req.Enabled {
		if xerr := h.hasAccess(c, models.PermSendCampaigns, models.APIPermSendCampaigns); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
	}
	out, xerr := h.LeadReview.SetReview(c.Request.Context(), orgID, campaignID, *req.Enabled)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, map[string]string{
		"review_leads": strconv.FormatBool(*req.Enabled),
	}, map[string]string{
		"held":     strconv.Itoa(out.Held),
		"released": strconv.Itoa(out.Released),
	})
	c.JSON(http.StatusOK, out)
}

// ListCampaignReviewLeads pages the campaign's leads with their review state.
//
// GET /campaigns/:id/review/leads?status=&missing=&q=&cursor=&limit=
func (h *Handler) ListCampaignReviewLeads(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	f := models.CampaignReviewLeadsFilter{
		Status:  strings.TrimSpace(c.Query("status")),
		Missing: strings.TrimSpace(c.Query("missing")),
		Search:  c.Query("q"),
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be a number"))
			return
		}
		f.Limit = n
	}
	if raw := c.Query("cursor"); raw != "" {
		cur, err := leadreview.DecodeCursor(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
			return
		}
		f.Cursor = cur
	}
	out, xerr := h.LeadReview.ListLeads(c.Request.Context(), orgID, campaignID, f)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

type reviewLeadsRequest struct {
	ContactIDs []uuid.UUID `json:"contact_ids"`
	All        bool        `json:"all"`
}

// ApproveCampaignReviewLeads releases waiting leads so their first email can
// go out. Approving a lead that is not waiting changes nothing, so a retry is safe.
//
// POST /campaigns/:id/review/approve
func (h *Handler) ApproveCampaignReviewLeads(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	var req reviewLeadsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.LeadReview.Approve(c.Request.Context(), orgID, campaignID, req.ContactIDs, req.All)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditReviewLeads(c, models.AuditActionApprove, campaignID, req, out.Released)
	c.JSON(http.StatusOK, out)
}

// ReopenCampaignReviewLeads puts leads not yet emailed back into review.
// Reopening a lead already waiting changes nothing, so a retry is safe.
//
// POST /campaigns/:id/review/reopen
func (h *Handler) ReopenCampaignReviewLeads(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	var req reviewLeadsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if req.All {
		errx.JSON(c, errx.New(errx.BadRequest, "reopen takes contact_ids; turn review mode on to hold every lead"))
		return
	}
	out, xerr := h.LeadReview.Reopen(c.Request.Context(), orgID, campaignID, req.ContactIDs)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditReviewLeads(c, models.AuditActionPause, campaignID, req, out.Held)
	c.JSON(http.StatusOK, out)
}

// auditReviewLeads records a review write: per lead for one lead, as one campaign entry for a batch.
func (h *Handler) auditReviewLeads(c *gin.Context, action models.AuditAction, campaignID uuid.UUID, req reviewLeadsRequest, moved int) {
	meta := map[string]string{"campaign_id": campaignID.String(), "source": "review", "leads": strconv.Itoa(moved)}
	if len(req.ContactIDs) == 1 {
		id := req.ContactIDs[0]
		h.auditOrg(c, action, models.AuditEntityCampaignLead, &id, nil, meta)
		return
	}
	h.auditOrg(c, action, models.AuditEntityCampaignLead, nil, nil, meta)
}

func leadStepParams(c *gin.Context, withStep bool) (contactID, stepID uuid.UUID, ok bool) {
	if contactID, ok = parseUUIDParam(c, "contactId"); !ok {
		return
	}
	if withStep {
		stepID, ok = parseUUIDParam(c, "stepId")
	}
	return
}

// GetCampaignLeadEmails renders every email step for one lead as it will be sent.
//
// GET /campaigns/:id/leads/:contactId/emails
func (h *Handler) GetCampaignLeadEmails(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	contactID, _, ok := leadStepParams(c, false)
	if !ok {
		return
	}
	out, xerr := h.LeadReview.LeadEmails(c.Request.Context(), orgID, campaignID, contactID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// PutCampaignLeadEmail stores this lead's own copy of one email step. It
// replaces the copy as a whole, so a retry lands on the same row.
//
// PUT /campaigns/:id/leads/:contactId/emails/:stepId
func (h *Handler) PutCampaignLeadEmail(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	contactID, stepID, ok := leadStepParams(c, true)
	if !ok {
		return
	}
	var req models.LeadStepOverrideInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	var editor *uuid.UUID
	if id, err := middleware.GetUserUUID(c); err == nil && id != uuid.Nil {
		editor = &id
	}
	out, xerr := h.LeadReview.Personalize(c.Request.Context(), orgID, campaignID, contactID, stepID, editor, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaignLead, &contactID, nil, map[string]string{
		"campaign_id": campaignID.String(), "step_id": stepID.String(), "personalized": "true",
	})
	c.JSON(http.StatusOK, out)
}

// DeleteCampaignLeadEmail drops this lead's own copy, so the step's copy goes
// out again. Deleting a copy that is not there is a success.
//
// DELETE /campaigns/:id/leads/:contactId/emails/:stepId
func (h *Handler) DeleteCampaignLeadEmail(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	contactID, stepID, ok := leadStepParams(c, true)
	if !ok {
		return
	}
	removed, xerr := h.LeadReview.ResetPersonalization(c.Request.Context(), orgID, campaignID, contactID, stepID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if removed {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaignLead, &contactID, nil, map[string]string{
			"campaign_id": campaignID.String(), "step_id": stepID.String(), "personalized": "false",
		})
	}
	c.Status(http.StatusNoContent)
}

// WriteCampaignLeadEmailAI writes one lead's AI copy for one step now. The
// copy is cached where the send reads it, so a retry writes and charges nothing
// more; rewrite: true replaces it on purpose and is charged again.
//
// POST /campaigns/:id/leads/:contactId/emails/:stepId/write
func (h *Handler) WriteCampaignLeadEmailAI(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	contactID, stepID, ok := leadStepParams(c, true)
	if !ok {
		return
	}
	var req struct {
		// Rewrite replaces copy already written; it is charged again.
		Rewrite bool `json:"rewrite"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			errx.JSON(c, errx.InvalidBody(err))
			return
		}
	}
	if xerr := h.LeadReview.WriteAI(c.Request.Context(), orgID, campaignID, contactID, stepID, req.Rewrite); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	out, xerr := h.LeadReview.LeadEmails(c.Request.Context(), orgID, campaignID, contactID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

type writeReviewAIRequest struct {
	StepID     *uuid.UUID  `json:"step_id"`
	ContactIDs []uuid.UUID `json:"contact_ids"`
	// WaitingOnly limits the pass to leads waiting for review.
	WaitingOnly bool `json:"waiting_only"`
	// After is the previous pass's next cursor.
	After string `json:"after"`
}

// WriteCampaignReviewAI writes AI copy ahead for one pass of leads still
// missing it. Each pass skips what is already written, so a retry charges
// nothing twice; call it again with the returned next as after while remaining is above zero.
//
// POST /campaigns/:id/review/write
func (h *Handler) WriteCampaignReviewAI(c *gin.Context) {
	orgID, campaignID, ok := h.reviewReady(c)
	if !ok {
		return
	}
	var req writeReviewAIRequest
	// Present but null or empty is refused, so a client bug cannot widen a paid pass to every lead.
	var probe struct {
		ContactIDs json.RawMessage `json:"contact_ids"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
			errx.JSON(c, errx.InvalidBody(err))
			return
		}
		_ = c.ShouldBindBodyWith(&probe, binding.JSON)
	}
	if probe.ContactIDs != nil && len(req.ContactIDs) == 0 {
		errx.JSON(c, errx.New(errx.BadRequest, "contact_ids is empty; omit it to write every lead"))
		return
	}
	var after *models.CampaignReviewCursor
	if req.After != "" {
		cur, err := leadreview.DecodeCursor(req.After)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid after cursor"))
			return
		}
		after = cur
	}
	out, xerr := h.LeadReview.WriteAIBatch(c.Request.Context(), orgID, campaignID, req.StepID, req.ContactIDs, req.WaitingOnly, after)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}
