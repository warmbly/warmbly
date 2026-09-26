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
)

func (h *Handler) placementReady(c *gin.Context) bool {
	if h.PlacementService == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "placement testing is not configured"))
		return false
	}
	return true
}

// placementLimit parses ?limit into [1, 100], 400 on anything else.
func placementLimit(c *gin.Context) (int, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return 25, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid limit"))
		return 0, false
	}
	return n, true
}

func optionalUUID(c *gin.Context, raw, field string) (*uuid.UUID, bool) {
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid "+field))
		return nil, false
	}
	return &id, true
}

// --- Workspace ----------------------------------------------------------

// GetPlacementOverview lists the seed panels this workspace can test on and
// its monthly allowance.
func (h *Handler) GetPlacementOverview(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	out, xerr := h.PlacementService.Overview(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

type createPlacementTestRequest struct {
	SenderAccountID string `json:"sender_account_id"`
	CampaignID      string `json:"campaign_id"`
	SequenceID      string `json:"sequence_id"`
	ContactID       string `json:"contact_id"`
	Subject         string `json:"subject"`
	BodyHTML        string `json:"body_html"`
	BodyPlain       string `json:"body_plain"`
	Tracking        string `json:"tracking"`
	Panel           string `json:"panel"`
}

// CreatePlacementTest starts a test: one test, or two for a tracking
// comparison.
func (h *Handler) CreatePlacementTest(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	var req createPlacementTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	senderID, err := uuid.Parse(req.SenderAccountID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid sender_account_id"))
		return
	}
	if !middleware.APIKeyAllowsEmailAccount(c, senderID) {
		errx.JSON(c, errx.New(errx.Forbidden, "this API key cannot send from that mailbox"))
		return
	}
	campaignID, ok := optionalUUID(c, req.CampaignID, "campaign_id")
	if !ok {
		return
	}
	sequenceID, ok := optionalUUID(c, req.SequenceID, "sequence_id")
	if !ok {
		return
	}
	contactID, ok := optionalUUID(c, req.ContactID, "contact_id")
	if !ok {
		return
	}
	var userID *uuid.UUID
	if id, err := middleware.GetUserUUID(c); err == nil && id != uuid.Nil {
		userID = &id
	}

	tests, xerr := h.PlacementService.CreateTests(c.Request.Context(), placement.CreateInput{
		OrgID:           *orgID,
		UserID:          userID,
		SenderAccountID: senderID,
		CampaignID:      campaignID,
		SequenceID:      sequenceID,
		ContactID:       contactID,
		Subject:         req.Subject,
		BodyHTML:        req.BodyHTML,
		BodyPlain:       req.BodyPlain,
		Tracking:        req.Tracking,
		Panel:           req.Panel,
		Origin:          models.PlacementOriginManual,
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	for _, t := range tests {
		id := t.ID
		h.auditOrg(c, models.AuditActionCreate, models.AuditEntityPlacementTest, &id, nil, map[string]string{
			"sender": t.SenderEmail,
			"panel":  t.Panel,
		})
	}
	c.JSON(http.StatusCreated, gin.H{"data": tests})
}

// ListPlacementTests lists the workspace's tests, newest first.
func (h *Handler) ListPlacementTests(c *gin.Context) {
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
	campaignID, ok := optionalUUID(c, c.Query("campaign_id"), "campaign_id")
	if !ok {
		return
	}
	tests, total, xerr := h.PlacementService.ListTests(c.Request.Context(), orgID, campaignID, limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tests, "pagination": pageMetaFor(offset, limit, len(tests), total)})
}

// GetPlacementTest returns one test with every copy, the content check and,
// for a tracking comparison, the other half.
func (h *Handler) GetPlacementTest(c *gin.Context) {
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
	detail, xerr := h.PlacementService.GetTest(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

// CancelPlacementTest stops the copies that have not been sent yet.
func (h *Handler) CancelPlacementTest(c *gin.Context) {
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
	view, xerr := h.PlacementService.CancelTest(c.Request.Context(), *orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionStop, models.AuditEntityPlacementTest, &id, nil, nil)
	c.JSON(http.StatusOK, gin.H{"data": view})
}

// ListPlacementSeeds lists the workspace's mailboxes and which are its seed
// inboxes.
func (h *Handler) ListPlacementSeeds(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	seeds, xerr := h.PlacementService.ListWorkspaceSeeds(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	allowed := seeds[:0]
	for _, s := range seeds {
		if middleware.APIKeyAllowsEmailAccount(c, s.EmailAccountID) {
			allowed = append(allowed, s)
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": allowed})
}

type setPlacementSeedRequest struct {
	Seed *bool `json:"seed"`
}

// SetPlacementSeed makes a workspace mailbox a seed inbox, or not.
func (h *Handler) SetPlacementSeed(c *gin.Context) {
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
	var req setPlacementSeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if req.Seed == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "seed is required"))
		return
	}
	seed, xerr := h.PlacementService.SetWorkspaceSeed(c.Request.Context(), *orgID, id, *req.Seed)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &id, map[string]string{
		"placement_seed": strconv.FormatBool(*req.Seed),
	}, map[string]string{"email": seed.Email})
	c.JSON(http.StatusOK, gin.H{"data": seed})
}

// GetPlacementMonitor returns a campaign's scheduled placement test, null when
// it has none.
func (h *Handler) GetPlacementMonitor(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	m, xerr := h.PlacementService.GetMonitor(c.Request.Context(), *orgID, campaignID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": m})
}

// PutPlacementMonitor creates or updates a campaign's scheduled test. A PUT of
// the same body lands on the same state, so it needs no Idempotency-Key.
func (h *Handler) PutPlacementMonitor(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var in placement.MonitorInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	userID, _ := middleware.GetUserUUID(c)
	m, xerr := h.PlacementService.PutMonitor(c.Request.Context(), *orgID, userID, campaignID, in)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	id := m.ID
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityPlacementMonitor, &id, nil, map[string]string{
		"campaign_id": campaignID.String(),
	})
	c.JSON(http.StatusOK, gin.H{"data": m})
}

// DeletePlacementMonitor removes a campaign's scheduled test.
func (h *Handler) DeletePlacementMonitor(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.placementReady(c) {
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if xerr := h.PlacementService.DeleteMonitor(c.Request.Context(), *orgID, campaignID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityPlacementMonitor, nil, nil, map[string]string{
		"campaign_id": campaignID.String(),
	})
	c.Status(http.StatusNoContent)
}

// --- Admin ----------------------------------------------------------------

type adminCreatePlacementTestRequest struct {
	SenderAccountID string `json:"sender_account_id"`
	Subject         string `json:"subject"`
	BodyPlain       string `json:"body_plain"`
	BodyHTML        string `json:"body_html"`
	Tracking        string `json:"tracking"`
}

// AdminCreatePlacementTest runs a test from any mailbox on the instance panel,
// outside every workspace allowance.
func (h *Handler) AdminCreatePlacementTest(c *gin.Context) {
	if !h.placementReady(c) {
		return
	}
	var req adminCreatePlacementTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	senderID, err := uuid.Parse(req.SenderAccountID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid sender_account_id"))
		return
	}
	if h.PlacementRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "placement testing is not configured"))
		return
	}
	sender, err := h.PlacementRepo.GetSeedAccount(c.Request.Context(), senderID)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if sender == nil || sender.OrganizationID == nil {
		errx.JSON(c, errx.New(errx.NotFound, "sending mailbox not found"))
		return
	}
	tests, xerr := h.PlacementService.CreateTests(c.Request.Context(), placement.CreateInput{
		OrgID:           *sender.OrganizationID,
		SenderAccountID: senderID,
		Subject:         req.Subject,
		BodyHTML:        req.BodyHTML,
		BodyPlain:       req.BodyPlain,
		Tracking:        req.Tracking,
		Panel:           models.PlacementPanelInstance,
		Origin:          models.PlacementOriginAdmin,
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	for _, t := range tests {
		id := t.ID
		h.audit(c, models.AuditActionCreate, models.AuditEntityPlacementTest, &id, map[string]string{
			"sender_account_id": senderID.String(),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": tests})
}

// AdminListPlacementTests lists every workspace's tests, newest first.
func (h *Handler) AdminListPlacementTests(c *gin.Context) {
	if !h.placementReady(c) {
		return
	}
	offset, ok := decodeOffsetCursor(c.Query("cursor"))
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
		return
	}
	limit := parseLimit(c.Query("limit"), 25)
	tests, total, xerr := h.PlacementService.ListTests(c.Request.Context(), nil, nil, limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tests, "pagination": pageMetaFor(offset, limit, len(tests), total)})
}

// AdminGetPlacementTest returns any test in full.
func (h *Handler) AdminGetPlacementTest(c *gin.Context) {
	if !h.placementReady(c) {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	detail, xerr := h.PlacementService.GetTest(c.Request.Context(), nil, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

// AdminListSeedMailboxes lists the instance panel.
func (h *Handler) AdminListSeedMailboxes(c *gin.Context) {
	if !h.placementReady(c) {
		return
	}
	seeds, xerr := h.PlacementService.AdminListSeeds(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": seeds})
}

// AdminListSeedCandidates searches connected mailboxes to add to the panel.
func (h *Handler) AdminListSeedCandidates(c *gin.Context) {
	if !h.placementReady(c) {
		return
	}
	seeds, xerr := h.PlacementService.AdminSeedCandidates(c.Request.Context(), c.Query("search"), parseLimit(c.Query("limit"), 50))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": seeds})
}

// AdminSetSeedMailbox adds a mailbox to the instance panel or takes it off.
func (h *Handler) AdminSetSeedMailbox(c *gin.Context) {
	if !h.placementReady(c) {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var req setPlacementSeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if req.Seed == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "seed is required"))
		return
	}
	seed, xerr := h.PlacementService.AdminSetSeed(c.Request.Context(), id, *req.Seed)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.audit(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &id, map[string]string{
		"seed":  strconv.FormatBool(*req.Seed),
		"email": seed.Email,
	})
	c.JSON(http.StatusOK, gin.H{"data": seed})
}

// --- Warmbly Cloud, for linked instances -----------------------------------

// PoolLinkPlacementPanel is the cloud seed panel as a linked instance sees it.
func (h *Handler) PoolLinkPlacementPanel(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if !h.placementReady(c) {
		return
	}
	out, xerr := h.PlacementService.RemotePanel(c.Request.Context(), inst)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// PoolLinkStartPlacement hands a linked instance the seeds for a test.
func (h *Handler) PoolLinkStartPlacement(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if !h.placementReady(c) {
		return
	}
	var req models.PlacementCloudStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.PlacementService.RemoteStart(c.Request.Context(), inst, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// PoolLinkPlacementSends records the Message-IDs of the copies an instance
// sent.
func (h *Handler) PoolLinkPlacementSends(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if !h.placementReady(c) {
		return
	}
	testID, err := uuid.Parse(c.Param("testId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid test id"))
		return
	}
	var req models.PlacementCloudSends
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if xerr := h.PlacementService.RemoteSends(c.Request.Context(), inst, testID, req.Sends); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}

// PoolLinkPlacementVerdicts reports where each copy landed so far.
func (h *Handler) PoolLinkPlacementVerdicts(c *gin.Context) {
	inst := middleware.GetPoolLinkInstance(c)
	if inst == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if !h.placementReady(c) {
		return
	}
	testID, err := uuid.Parse(c.Param("testId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid test id"))
		return
	}
	out, xerr := h.PlacementService.RemoteGet(c.Request.Context(), inst, testID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}
