package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type placementBatchRequest struct {
	SenderAccountIDs []uuid.UUID                  `json:"sender_account_ids"`
	SenderScope      *models.PlacementSenderScope `json:"sender_scope"`
	Sample           models.PlacementSample       `json:"sample"`
	CampaignID       string                       `json:"campaign_id"`
	SequenceID       string                       `json:"sequence_id"`
	ContactID        string                       `json:"contact_id"`
	Subject          string                       `json:"subject"`
	BodyHTML         string                       `json:"body_html"`
	BodyPlain        string                       `json:"body_plain"`
	Tracking         string                       `json:"tracking"`
	Panel            string                       `json:"panel"`
	Pace             string                       `json:"pace"`
	Families         []string                     `json:"families"`
	SeedIDs          []uuid.UUID                  `json:"seed_ids"`
	OnUnavailable    string                       `json:"on_unavailable"`
	MaxCredits       int                          `json:"max_credits"`
}

// placementBatchInput binds a batch request; false after answering the error.
func (h *Handler) placementBatchInput(c *gin.Context) (placement.BatchInput, bool) {
	var in placement.BatchInput
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return in, false
	}
	if !h.placementReady(c) {
		return in, false
	}
	var req placementBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return in, false
	}
	campaignID, ok := optionalUUID(c, req.CampaignID, "campaign_id")
	if !ok {
		return in, false
	}
	sequenceID, ok := optionalUUID(c, req.SequenceID, "sequence_id")
	if !ok {
		return in, false
	}
	contactID, ok := optionalUUID(c, req.ContactID, "contact_id")
	if !ok {
		return in, false
	}
	in = placement.BatchInput{
		OrgID:            *orgID,
		SenderAccountIDs: req.SenderAccountIDs,
		Scope:            req.SenderScope,
		Sample:           req.Sample,
		CampaignID:       campaignID,
		SequenceID:       sequenceID,
		ContactID:        contactID,
		Subject:          req.Subject,
		BodyHTML:         req.BodyHTML,
		BodyPlain:        req.BodyPlain,
		Tracking:         req.Tracking,
		Panel:            req.Panel,
		Pace:             req.Pace,
		Families:         req.Families,
		SeedIDs:          req.SeedIDs,
		OnUnavailable:    req.OnUnavailable,
		MaxCredits:       req.MaxCredits,
	}
	// A caller limited to some mailboxes only ever tests from those.
	if allowed := middleware.AllowedEmailAccounts(c); len(allowed) > 0 {
		in.AllowedSenders = allowed
	}
	if id, err := middleware.GetUserUUID(c); err == nil && id != uuid.Nil {
		in.UserID = &id
	}
	return in, true
}

// PreviewPlacementBatch reports how many senders, tests, sends and credits a
// batch would come to. It starts nothing.
func (h *Handler) PreviewPlacementBatch(c *gin.Context) {
	in, ok := h.placementBatchInput(c)
	if !ok {
		return
	}
	preview, xerr := h.PlacementService.PreviewBatch(c.Request.Context(), in)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preview})
}

// CreatePlacementBatch snapshots the senders and queues the batch; the
// backend starts its senders a few at a time.
func (h *Handler) CreatePlacementBatch(c *gin.Context) {
	in, ok := h.placementBatchInput(c)
	if !ok {
		return
	}
	batch, xerr := h.PlacementService.CreateBatch(c.Request.Context(), in)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	id := batch.ID
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityPlacementBatch, &id, nil, map[string]string{
		"senders": strconv.Itoa(batch.SenderCount),
		"panel":   batch.Panel,
	})
	c.JSON(http.StatusCreated, gin.H{"data": batch})
}

// ListPlacementBatches lists the workspace's batches, newest first.
func (h *Handler) ListPlacementBatches(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	offset, ok := decodeOffsetCursor(c.Query("cursor"))
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
		return
	}
	limit, ok := placementLimit(c)
	if !ok {
		return
	}
	batches, total, xerr := h.PlacementService.ListBatches(c.Request.Context(), *orgID, limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": batches, "pagination": pageMetaFor(offset, limit, len(batches), total)})
}

// GetPlacementBatch returns one batch with its placement overall and grouped
// by sending domain, sending provider and recipient provider.
func (h *Handler) GetPlacementBatch(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	detail, xerr := h.PlacementService.GetBatch(c.Request.Context(), *orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

// ListPlacementBatchSenders lists a batch's senders with where each one's
// copies landed, worst inbox rate first unless sorted otherwise.
func (h *Handler) ListPlacementBatchSenders(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	offset, ok := decodeOffsetCursor(c.Query("cursor"))
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
		return
	}
	limit, ok := placementLimit(c)
	if !ok {
		return
	}
	senders, total, xerr := h.PlacementService.ListBatchSenders(c.Request.Context(), *orgID, id, repository.PlacementBatchSenderFilter{
		Status: c.Query("status"),
		Search: c.Query("q"),
		Sort:   c.Query("sort"),
		Limit:  limit,
		Offset: offset,
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": senders, "pagination": pageMetaFor(offset, limit, len(senders), total)})
}

// CancelPlacementBatch stops a batch. Copies already sent keep being
// classified.
func (h *Handler) CancelPlacementBatch(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	view, xerr := h.PlacementService.CancelBatch(c.Request.Context(), *orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionStop, models.AuditEntityPlacementBatch, &id, nil, nil)
	c.JSON(http.StatusOK, gin.H{"data": view})
}

// GetPlacementCoverage reports how much of the workspace's connected fleet
// delivered a placement test in the last 7 and 30 days.
func (h *Handler) GetPlacementCoverage(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	cov, xerr := h.PlacementService.Coverage(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cov})
}
