package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// segmentScope pulls the org and the segment id (when the route has one).
func segmentScope(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	raw := c.Param("id")
	if raw == "" {
		return *orgID, uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid segment id"))
		return uuid.Nil, uuid.Nil, false
	}
	return *orgID, id, true
}

func (h *Handler) ListSegments(c *gin.Context) {
	orgID, _, ok := segmentScope(c)
	if !ok {
		return
	}
	out, xerr := h.SegmentService.List(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *Handler) ListSegmentFields(c *gin.Context) {
	orgID, _, ok := segmentScope(c)
	if !ok {
		return
	}
	out, xerr := h.SegmentService.Fields(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *Handler) GetSegment(c *gin.Context) {
	orgID, id, ok := segmentScope(c)
	if !ok {
		return
	}
	out, xerr := h.SegmentService.Get(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) CreateSegment(c *gin.Context) {
	orgID, _, ok := segmentScope(c)
	if !ok {
		return
	}
	var in models.SegmentWrite
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	var createdBy *uuid.UUID
	if uid, err := middleware.GetUserUUID(c); err == nil {
		createdBy = &uid
	}
	out, xerr := h.SegmentService.Create(c.Request.Context(), orgID, createdBy, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntitySegment, &out.ID, nil, map[string]string{"name": out.Name})
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) UpdateSegment(c *gin.Context) {
	orgID, id, ok := segmentScope(c)
	if !ok {
		return
	}
	var in models.SegmentWrite
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	out, xerr := h.SegmentService.Update(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySegment, &out.ID, nil, map[string]string{"name": out.Name})
	c.JSON(http.StatusOK, out)
}

func (h *Handler) DeleteSegment(c *gin.Context) {
	orgID, id, ok := segmentScope(c)
	if !ok {
		return
	}
	if xerr := h.SegmentService.Delete(c.Request.Context(), orgID, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntitySegment, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// PreviewSegment counts the contacts an unsaved definition would match.
func (h *Handler) PreviewSegment(c *gin.Context) {
	orgID, _, ok := segmentScope(c)
	if !ok {
		return
	}
	var in models.SegmentPreview
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	n, xerr := h.SegmentService.Preview(c.Request.Context(), orgID, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"contact_count": n})
}

// SetSegmentMembers writes a manual include/exclude override (or clears it).
func (h *Handler) SetSegmentMembers(c *gin.Context) {
	orgID, id, ok := segmentScope(c)
	if !ok {
		return
	}
	var in models.SegmentMembersWrite
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	// "Select all matching" names a filter instead of listing ids; resolve it
	// here so the segment service only ever sees a concrete batch.
	ids, ok := h.resolveContactSelection(c, orgID, in.ContactSelection)
	if !ok {
		return
	}
	in.ContactSelection = models.ContactSelection{Contacts: ids}

	n, xerr := h.SegmentService.SetMembers(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySegment, &id, nil, map[string]string{"members": string(in.Mode)})
	c.JSON(http.StatusOK, gin.H{"updated": n})
}

// GetSegmentMemberModes reports the manual override of the given contacts.
func (h *Handler) GetSegmentMemberModes(c *gin.Context) {
	orgID, id, ok := segmentScope(c)
	if !ok {
		return
	}
	var in struct {
		Contacts []string `json:"contacts"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	modes, xerr := h.SegmentService.MemberModes(c.Request.Context(), orgID, id, in.Contacts)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	out := map[string]models.SegmentMemberMode{}
	for k, v := range modes {
		out[k.String()] = v
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// AddSegmentToCampaign enrols the segment's current members as campaign leads.
func (h *Handler) AddSegmentToCampaign(c *gin.Context) {
	orgID, id, ok := segmentScope(c)
	if !ok {
		return
	}
	var in models.SegmentAddToCampaign
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	res, xerr := h.SegmentService.AddToCampaign(c.Request.Context(), orgID, middleware.GetUserID(c), id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &res.CampaignID, nil, map[string]string{"segment_id": id.String(), "added": itoa(res.Added)})
	c.JSON(http.StatusOK, res)
}

// campaignSegmentScope pulls the org and the campaign id from a campaigns route.
func campaignSegmentScope(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid campaign id"))
		return uuid.Nil, uuid.Nil, false
	}
	return *orgID, id, true
}

// ListCampaignSegments lists the segments linked to a campaign.
func (h *Handler) ListCampaignSegments(c *gin.Context) {
	orgID, campaignID, ok := campaignSegmentScope(c)
	if !ok {
		return
	}
	out, xerr := h.SegmentService.ListCampaignSegments(c.Request.Context(), orgID, campaignID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// SetCampaignSegments replaces a campaign's linked segments. Members of the
// linked segments are enrolled as leads immediately and kept current; a
// detached segment's leads are withdrawn again unless the campaign has already
// written to them or somebody added them by hand.
func (h *Handler) SetCampaignSegments(c *gin.Context) {
	orgID, campaignID, ok := campaignSegmentScope(c)
	if !ok {
		return
	}
	var in models.CampaignSegmentsWrite
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	// An omitted field must not read as "detach everything"; only an
	// explicit [] does that.
	if in.SegmentIDs == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "segment_ids is required; send [] to detach all segments"))
		return
	}
	links, added, change, xerr := h.SegmentService.SetCampaignSegments(c.Request.Context(), orgID, campaignID, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, map[string]string{
		"segments": itoa(len(links)), "added": itoa(added), "withdrawn": itoa(change.Withdrawn),
	})
	c.JSON(http.StatusOK, gin.H{"data": links, "added": added, "withdrawn": change.Withdrawn, "contacted": change.Contacted})
}

// ListSegmentOverrides lists the contacts pinned into or out of a segment.
func (h *Handler) ListSegmentOverrides(c *gin.Context) {
	orgID, id, ok := segmentScope(c)
	if !ok {
		return
	}
	out, xerr := h.SegmentService.Overrides(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// ListContactSegments reports every segment of the org with whether this
// contact is a member and any manual override on it.
func (h *Handler) ListContactSegments(c *gin.Context) {
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	out, xerr := h.SegmentService.SegmentsForContact(c.Request.Context(), *orgID, contactID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}
