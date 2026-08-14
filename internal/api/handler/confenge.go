package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/confenge"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// confengeOrg resolves the org when CONFENGE outreach is wired and enabled.
func (h *Handler) confengeOrg(c *gin.Context) (uuid.UUID, bool) {
	if h.ConfengeService == nil || !h.ConfengeService.Enabled() {
		errx.JSON(c, errx.New(errx.NotFound, "CONFENGE outreach is not enabled on this server"))
		return uuid.Nil, false
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, false
	}
	return *orgID, true
}

// GetConfengeStatus — GET /confenge/status
// Reports whether the feature is on (always 200 when authenticated with org)
// and includes the discrete operator readiness panel.
func (h *Handler) GetConfengeStatus(c *gin.Context) {
	enabled := h.ConfengeService != nil && h.ConfengeService.Enabled()
	cfg := confenge.Config{}
	if h.ConfengeService != nil {
		cfg = h.ConfengeService.Config()
	}
	emailReady := false
	var readiness confenge.Readiness
	if h.ConfengeService != nil {
		orgID := middleware.GetOrganizationID(c)
		if orgID != nil && enabled {
			if h.EmailService != nil {
				if res, xerr := h.EmailService.Search(c.Request.Context(), orgID.String(), "", "", "", "10", nil); xerr == nil && res != nil {
					for _, e := range res.Data {
						if strings.EqualFold(e.Status, "active") {
							emailReady = true
							break
						}
					}
				}
			}
			readiness = h.ConfengeService.CollectReadiness(c.Request.Context(), *orgID, emailReady)
		} else {
			readiness = confenge.BuildReadiness(cfg, confenge.ReadinessInputs{EmailReady: emailReady})
		}
	} else {
		readiness = confenge.BuildReadiness(cfg, confenge.ReadinessInputs{})
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":                 enabled,
		"auto_send_enabled":       enabled && cfg.AutoSendEnabled,
		"require_human_approval":  cfg.RequireHumanApproval,
		"default_daily_limit":     cfg.DefaultDailyLimit,
		"max_initial_email_words": cfg.MaxInitialEmailWords,
		"feed_configured":         enabled && cfg.FeedURL != "",
		"kill_switch":             !cfg.SendingAllowed(),
		"sending_allowed":         cfg.SendingAllowed(),
		"readiness":               readiness,
	})
}

// GetConfengeSummary — GET /confenge/summary
func (h *Handler) GetConfengeSummary(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	sum, xerr := h.ConfengeService.Summary(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": sum})
}

// ListConfengeAccounts — GET /confenge/accounts
func (h *Handler) ListConfengeAccounts(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	filter := repository.OutreachAccountFilter{
		QueueState:      c.Query("queue_state"),
		CNPJ14:          c.Query("cnpj14"),
		Search:          c.Query("q"),
		ActivationState: c.Query("activation_state"),
	}
	if h.ConfengeService != nil && h.ConfengeService.Config().DynamicPriorityEnabled {
		filter.DynamicPriority = true
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		filter.Limit = n
	}
	if raw := c.Query("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid offset"))
			return
		}
		filter.Offset = n
	}
	list, xerr := h.ConfengeService.ListAccounts(c.Request.Context(), orgID, filter)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// GetConfengeWorkingOverview — GET /confenge/working-overview
// Reservoir / agora / needs-contact / capacity planning metrics (not governor).
func (h *Handler) GetConfengeWorkingOverview(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	ov, xerr := h.ConfengeService.WorkingQueueOverview(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ov})
}

// ListConfengeWorkingQueue — GET /confenge/working-queue?lane=agora|needs_contact|...
func (h *Handler) ListConfengeWorkingQueue(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	lane := c.Query("lane")
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		limit = n
	}
	list, xerr := h.ConfengeService.ListWorkingQueue(c.Request.Context(), orgID, lane, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// SyncConfengeFeed — POST /confenge/sync
// Pulls extra-cli manifest + chunks, validates hashes, imports idempotently.
func (h *Handler) SyncConfengeFeed(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "user required"))
		return
	}
	var body struct {
		ManifestURI string `json:"manifest_uri"`
	}
	_ = c.ShouldBindJSON(&body)
	uid := userID
	res, xerr := h.ConfengeService.SyncFeedManifest(c.Request.Context(), orgID, &uid, body.ManifestURI)
	if xerr != nil {
		if res != nil {
			errx.JSON(c, xerr)
			return
		}
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// GetConfengeAccount — GET /confenge/accounts/:id
func (h *Handler) GetConfengeAccount(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	acc, xerr := h.ConfengeService.GetAccount(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": acc})
}

// BlockConfengeAccount — POST /confenge/accounts/:id/block
func (h *Handler) BlockConfengeAccount(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		Reason       string `json:"reason"`
		DoNotContact bool   `json:"do_not_contact"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && err.Error() != "EOF" {
		// empty body is fine
		if c.Request.ContentLength != 0 {
			errx.JSON(c, errx.ErrInvalid)
			return
		}
	}
	acc, xerr := h.ConfengeService.BlockAccount(c.Request.Context(), orgID, userID, id, body.Reason, body.DoNotContact)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": acc})
}

// ImportConfengeFeed — POST /confenge/import
// Accepts a raw feed body (native confenge.outreach.v1 or legacy) or
// {"uri":"https://...","dry_run":true}. Honour Idempotency-Key.
func (h *Handler) ImportConfengeFeed(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	var userPtr *uuid.UUID
	if uid, err := middleware.GetUserUUID(c); err == nil {
		userPtr = &uid
	}
	opts := confenge.ImportOptions{
		DryRun:         queryBool(c, "dry_run"),
		IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")),
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 33<<20))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "failed to read body"))
		return
	}
	if len(raw) == 0 {
		// Empty body: import from configured feed URL.
		run, xerr := h.ConfengeService.ImportFromURI(c.Request.Context(), orgID, userPtr, "", opts)
		if xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": run})
		return
	}

	// Envelope: fetch by URI without embedding the full feed.
	var env struct {
		URI           string          `json:"uri"`
		DryRun        *bool           `json:"dry_run"`
		SchemaVersion string          `json:"schema_version"`
		Leads         json.RawMessage `json:"leads"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.URI != "" && env.SchemaVersion == "" && len(env.Leads) == 0 {
		if env.DryRun != nil {
			opts.DryRun = *env.DryRun
		}
		run, xerr := h.ConfengeService.ImportFromURI(c.Request.Context(), orgID, userPtr, env.URI, opts)
		if xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": run})
		return
	}

	run, xerr := h.ConfengeService.ImportFromBytes(c.Request.Context(), orgID, userPtr, raw, opts)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": run})
}

// ListConfengeImportRuns — GET /confenge/import-runs
func (h *Handler) ListConfengeImportRuns(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	list, xerr := h.ConfengeService.ListImportRuns(c.Request.Context(), orgID, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// GetConfengeImportRun — GET /confenge/import-runs/:id
func (h *Handler) GetConfengeImportRun(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	run, xerr := h.ConfengeService.GetImportRun(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": run})
}

func queryBool(c *gin.Context, key string) bool {
	v := strings.ToLower(strings.TrimSpace(c.Query(key)))
	return v == "1" || v == "true" || v == "yes"
}

// ListConfengeDrafts — GET /confenge/drafts
func (h *Handler) ListConfengeDrafts(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	limit, offset := 50, 0
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if raw := c.Query("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			offset = n
		}
	}
	list, xerr := h.ConfengeService.ListDrafts(c.Request.Context(), orgID, c.Query("status"), limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// GetConfengeDraft — GET /confenge/drafts/:id
func (h *Handler) GetConfengeDraft(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	d, xerr := h.ConfengeService.GetDraft(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": d})
}

// GenerateConfengeDraft — POST /confenge/accounts/:id/generate
func (h *Handler) GenerateConfengeDraft(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		ContactCandidateID *uuid.UUID `json:"contact_candidate_id"`
	}
	_ = c.ShouldBindJSON(&body)
	d, xerr := h.ConfengeService.GenerateDraft(c.Request.Context(), orgID, userID, accountID, body.ContactCandidateID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": d})
}

// ReviewConfengeDraft — POST /confenge/drafts/:id/review
// Body: {"action":"approve|reject|skip|edit|block", "subject":"...", "body_text":"...", "do_not_contact":false}
func (h *Handler) ReviewConfengeDraft(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		Action       string  `json:"action" binding:"required"`
		Subject      *string `json:"subject"`
		BodyText     *string `json:"body_text"`
		Reason       *string `json:"reason"`
		DoNotContact bool    `json:"do_not_contact"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	edit := &confenge.DraftEdit{
		Subject: body.Subject, BodyText: body.BodyText,
		Reason: body.Reason, DoNotContact: body.DoNotContact,
	}
	d, xerr := h.ConfengeService.ReviewDraft(c.Request.Context(), orgID, userID, id, body.Action, edit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": d})
}

// BootstrapConfengeCampaign — POST /confenge/campaign/bootstrap
func (h *Handler) BootstrapConfengeCampaign(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	camp, xerr := h.ConfengeService.BootstrapCampaign(c.Request.Context(), orgID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": camp})
}

// EnrollConfengeDraft — POST /confenge/drafts/:id/enroll
func (h *Handler) EnrollConfengeDraft(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	d, xerr := h.ConfengeService.EnrollDraft(c.Request.Context(), orgID, userID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": d})
}

// BootstrapConfengePipeline — POST /confenge/crm/bootstrap
func (h *Handler) BootstrapConfengePipeline(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	pipe, xerr := h.ConfengeService.BootstrapPipeline(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pipe})
}

// DecideConfengeChannel returns the deterministic multichannel decision for an account.
func (h *Handler) DecideConfengeChannel(c *gin.Context) {
	if h.ConfengeService == nil {
		errx.JSON(c, errx.New(errx.NotFound, "confenge disabled"))
		return
	}
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid account id"))
		return
	}
	var contactID *uuid.UUID
	if s := strings.TrimSpace(c.Query("contact_id")); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid contact_id"))
			return
		}
		contactID = &id
	}
	d, xerr := h.ConfengeService.DecideChannel(c.Request.Context(), orgID, accountID, contactID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, d)
}

// GenerateConfengeWhatsAppDraft creates a short WhatsApp draft for human review.
func (h *Handler) GenerateConfengeWhatsAppDraft(c *gin.Context) {
	if h.ConfengeService == nil {
		errx.JSON(c, errx.New(errx.NotFound, "confenge disabled"))
		return
	}
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid account id"))
		return
	}
	var contactID *uuid.UUID
	if s := strings.TrimSpace(c.Query("contact_id")); s != "" {
		id, err := uuid.Parse(s)
		if err == nil {
			contactID = &id
		}
	}
	d, xerr := h.ConfengeService.GenerateWhatsAppDraft(c.Request.Context(), orgID, uid, accountID, contactID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, d)
}

// SendConfengeWhatsAppDraft sends an APPROVED WhatsApp draft via Evolution after policy gate.
func (h *Handler) SendConfengeWhatsAppDraft(c *gin.Context) {
	if h.ConfengeService == nil {
		errx.JSON(c, errx.New(errx.NotFound, "confenge disabled"))
		return
	}
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	draftID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid draft id"))
		return
	}
	d, xerr := h.ConfengeService.SendApprovedWhatsApp(c.Request.Context(), orgID, uid, draftID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, d)
}

// GetConfengeDispatchStatus — GET /confenge/dispatch/status
func (h *Handler) GetConfengeDispatchStatus(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	st, xerr := h.ConfengeService.DispatchStatus(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

// PauseConfengeDispatch — POST /confenge/dispatch/pause
func (h *Handler) PauseConfengeDispatch(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	if xerr := h.ConfengeService.PauseDispatch(c.Request.Context(), orgID, uid, body.Reason); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	st, _ := h.ConfengeService.DispatchStatus(c.Request.Context(), orgID)
	c.JSON(http.StatusOK, gin.H{"data": st})
}

// InvalidatePriorComposerDrafts — POST /confenge/drafts/invalidate-prior-composer
func (h *Handler) InvalidatePriorComposerDrafts(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	rep, xerr := h.ConfengeService.InvalidatePriorComposerDrafts(c.Request.Context(), orgID, uid)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rep})
}

func (h *Handler) GetConfengeContactCockpit(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	cockpit, xerr := h.ConfengeService.CollectContactCockpit(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cockpit})
}

func (h *Handler) GetConfengeToday(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	today, xerr := h.ConfengeService.CollectToday(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": today})
}

func (h *Handler) StartConfengeCommercialAction(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	actionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	a, xerr := h.ConfengeService.StartCommercialWork(c.Request.Context(), orgID, uid, actionID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *Handler) RecordConfengeCommercialOutcome(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	actionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	var body struct {
		OutcomeCode             string `json:"outcome_code"`
		Notes                   string `json:"notes"`
		ReferralName            string `json:"referral_name"`
		ReferralRole            string `json:"referral_role"`
		ReferralChannel         string `json:"referral_channel"`
		NextActionType          string `json:"next_action_type"`
		NextActionAt            string `json:"next_action_at"`
		RouteQualityFeedback    string `json:"route_quality_feedback"`
		PersonRelevanceFeedback string `json:"person_relevance_feedback"`
		MessageFeedback         string `json:"message_feedback"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid body"))
		return
	}
	req := confenge.OutcomeRequest{
		OutcomeCode:             body.OutcomeCode,
		Notes:                   body.Notes,
		ReferralName:            body.ReferralName,
		ReferralRole:            body.ReferralRole,
		ReferralChannel:         body.ReferralChannel,
		NextActionType:          body.NextActionType,
		RouteQualityFeedback:    body.RouteQualityFeedback,
		PersonRelevanceFeedback: body.PersonRelevanceFeedback,
		MessageFeedback:         body.MessageFeedback,
	}
	if ts := strings.TrimSpace(body.NextActionAt); ts != "" {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			req.NextActionAt = &parsed
		}
	}
	res, xerr := h.ConfengeService.RecordCommercialOutcome(c.Request.Context(), orgID, uid, actionID, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *Handler) ApplyConfengeManualAction(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid body"))
		return
	}
	hc, xerr := h.ConfengeService.ApplyManualAction(c.Request.Context(), orgID, uid, accountID, body.Action, body.Reason)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": hc})
}

// ResumeConfengeDispatch — POST /confenge/dispatch/resume
func (h *Handler) ResumeConfengeDispatch(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uuid.UUID)
	if xerr := h.ConfengeService.ResumeDispatch(c.Request.Context(), orgID, uid); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	st, _ := h.ConfengeService.DispatchStatus(c.Request.Context(), orgID)
	c.JSON(http.StatusOK, gin.H{"data": st})
}

func (h *Handler) ListConfengeReviewTouchpoints(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	limit, offset := 50, 0
	list, xerr := h.ConfengeService.ListReviewTouchpoints(c.Request.Context(), orgID, limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}
func (h *Handler) GetConfengeTouchpoint(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	tp, xerr := h.ConfengeService.GetTouchpoint(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tp})
}
func (h *Handler) ListConfengeAccountTouchpoints(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	list, xerr := h.ConfengeService.ListAccountTouchpoints(c.Request.Context(), orgID, accountID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}
func (h *Handler) PlanConfengeCadence(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		ContactCandidateID *uuid.UUID `json:"contact_candidate_id"`
		Channel            string     `json:"channel"`
	}
	_ = c.ShouldBindJSON(&body)
	list, xerr := h.ConfengeService.PlanAccountCadence(c.Request.Context(), orgID, userID, accountID, body.ContactCandidateID, body.Channel)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// PrepareConfengePilotCohort prepares up to 30 accounts independently for review.
func (h *Handler) PrepareConfengePilotCohort(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var body struct {
		AccountIDs []string `json:"account_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "account_ids is required"))
		return
	}
	if len(body.AccountIDs) == 0 || len(body.AccountIDs) > confenge.PilotCohortTarget {
		errx.JSON(c, errx.New(errx.BadRequest, "account_ids must contain between 1 and 30 accounts"))
		return
	}
	accountIDs := make([]uuid.UUID, 0, len(body.AccountIDs))
	seenAccountIDs := make(map[uuid.UUID]struct{}, len(body.AccountIDs))
	for _, raw := range body.AccountIDs {
		accountID, parseErr := uuid.Parse(strings.TrimSpace(raw))
		if parseErr != nil || accountID == uuid.Nil {
			errx.JSON(c, errx.New(errx.BadRequest, "account_ids contains an invalid UUID"))
			return
		}
		if _, duplicate := seenAccountIDs[accountID]; duplicate {
			errx.JSON(c, errx.New(errx.BadRequest, "account_ids must contain unique accounts"))
			return
		}
		seenAccountIDs[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}
	result, xerr := h.ConfengeService.PreparePilotCohort(c.Request.Context(), orgID, userID, accountIDs, confenge.PilotOperation{
		IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")),
		RequestID:      c.GetString("request_id"),
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
func (h *Handler) GenerateConfengeTouchpoint(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	tp, xerr := h.ConfengeService.GenerateTouchpointDraft(c.Request.Context(), orgID, userID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tp})
}
func (h *Handler) EditConfengeTouchpoint(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		Subject   *string `json:"subject"`
		BodyText  *string `json:"body_text"`
		Recipient *string `json:"recipient"`
		Channel   *string `json:"channel"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	tp, xerr := h.ConfengeService.EditTouchpoint(c.Request.Context(), orgID, userID, id, body.Subject, body.BodyText, body.Recipient, body.Channel)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tp})
}
func (h *Handler) ApproveConfengeTouchpoint(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		GenericRecipientAcknowledged bool `json:"generic_recipient_acknowledged"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid approval body"))
			return
		}
	}
	tp, xerr := h.ConfengeService.ApproveTouchpoint(c.Request.Context(), orgID, userID, id, confenge.ApprovalOptions{GenericRecipientAcknowledged: body.GenericRecipientAcknowledged})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tp})
}
func (h *Handler) RejectSkipConfengeTouchpoint(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		Action string `json:"action" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	tp, xerr := h.ConfengeService.RejectOrSkipTouchpointReason(c.Request.Context(), orgID, userID, id, body.Action, body.Reason)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tp})
}
func (h *Handler) QueueConfengeTouchpoint(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	tp, xerr := h.ConfengeService.QueueTouchpoint(c.Request.Context(), orgID, userID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tp})
}
func (h *Handler) CancelConfengeAccountTouchpoints(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Reason == "" {
		body.Reason = "CANCELLED"
	}
	n, xerr := h.ConfengeService.CancelAccountTouchpoints(c.Request.Context(), orgID, userID, accountID, body.Reason)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"cancelled": n}})
}
func (h *Handler) DNCConfengeAccount(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	_, xerr := h.ConfengeService.BlockAccount(c.Request.Context(), orgID, userID, accountID, "human_dnc", true)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	n, xerr := h.ConfengeService.CancelAccountTouchpoints(c.Request.Context(), orgID, userID, accountID, "DNC")
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"cancelled": n, "do_not_contact": true}})
}

// ListConfengeAttention — GET /confenge/attention?filter=needs_attention
func (h *Handler) ListConfengeAttention(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	filter := c.Query("filter")
	if filter == "" {
		filter = confenge.FilterNeedsAttention
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		limit = n
	}
	list, xerr := h.ConfengeService.ListAttention(c.Request.Context(), orgID, filter, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// GetConfengeAttention — GET /confenge/attention/:id
func (h *Handler) GetConfengeAttention(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	item, xerr := h.ConfengeService.GetAttention(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

// GenerateConfengeReplyDraft — POST /confenge/accounts/:id/generate-reply
func (h *Handler) GenerateConfengeReplyDraft(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		ContactCandidateID *uuid.UUID `json:"contact_candidate_id"`
	}
	_ = c.ShouldBindJSON(&body)
	d, xerr := h.ConfengeService.GenerateReplyDraft(c.Request.Context(), orgID, userID, accountID, body.ContactCandidateID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": d})
}

// ChangeConfengeReferral — POST /confenge/accounts/:id/referral
func (h *Handler) ChangeConfengeReferral(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
		Phone string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	cand, xerr := h.ConfengeService.ChangeReferralRecipient(c.Request.Context(), orgID, userID, accountID, body.Name, body.Email, body.Role, body.Phone)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cand})
}

// Body: {"resume_at":"2026-09-01T00:00:00Z","note":"..."} — never auto-reopens cadence.
func (h *Handler) ResumeConfengeAccount(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var body struct {
		ResumeAt string `json:"resume_at" binding:"required"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(body.ResumeAt))
	if err != nil {
		// accept date-only
		ts, err = time.ParseInLocation("2006-01-02", strings.TrimSpace(body.ResumeAt), time.UTC)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "resume_at must be RFC3339 or YYYY-MM-DD"))
			return
		}
	}
	acc, xerr := h.ConfengeService.ResumeAtDate(c.Request.Context(), orgID, userID, accountID, ts.UTC(), body.Note)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": acc})
}

// AuthorizeConfengeCampaignPolicy — POST /confenge/campaign/policy/authorize
func (h *Handler) AuthorizeConfengeCampaignPolicy(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var body struct {
		CampaignID               string `json:"campaign_id"`
		PromptPolicyVersion      string `json:"prompt_policy_version"`
		ValidatorVersion         string `json:"validator_version"`
		ContactPolicyVersion     string `json:"contact_policy_version"`
		TemplatePolicyVersion    string `json:"template_policy_version"`
		SenderMailbox            string `json:"sender_mailbox"`
		Channel                  string `json:"channel"`
		AllowedRiskClass         string `json:"allowed_risk_class"`
		MaxRatePerHour           int    `json:"max_rate_per_hour"`
		AllowPolicyTemplateGREEN bool   `json:"allow_policy_template_green"`
		AuthorizedByLabel        string `json:"authorized_by_label"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	cid, err := uuid.Parse(strings.TrimSpace(body.CampaignID))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "campaign_id required"))
		return
	}
	auth := &models.CampaignPolicyAuthorization{
		CampaignID:               cid,
		PromptPolicyVersion:      body.PromptPolicyVersion,
		ValidatorVersion:         body.ValidatorVersion,
		ContactPolicyVersion:     body.ContactPolicyVersion,
		TemplatePolicyVersion:    body.TemplatePolicyVersion,
		SenderMailbox:            body.SenderMailbox,
		Channel:                  body.Channel,
		AllowedRiskClass:         body.AllowedRiskClass,
		MaxRatePerHour:           body.MaxRatePerHour,
		AllowPolicyTemplateGREEN: body.AllowPolicyTemplateGREEN,
		AuthorizedByLabel:        body.AuthorizedByLabel,
		EffectiveAt:              time.Now().UTC(),
	}
	out, xerr := h.ConfengeService.AuthorizeCampaignPolicy(c.Request.Context(), orgID, userID, auth)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "authorization_mode": confenge.AuthorizationModeCampaignPolicy})
}

// GetConfengeCampaignPolicy — GET /confenge/campaign/policy?campaign_id=
func (h *Handler) GetConfengeCampaignPolicy(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	cid, err := uuid.Parse(strings.TrimSpace(c.Query("campaign_id")))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "campaign_id query required"))
		return
	}
	out, xerr := h.ConfengeService.GetActiveCampaignPolicy(c.Request.Context(), orgID, cid)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// GreenAutorunConfengeTouchpoint — POST /confenge/touchpoints/:id/green-autorun
func (h *Handler) GreenAutorunConfengeTouchpoint(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	tp, dec, xerr := h.ConfengeService.TryGreenAutorun(c.Request.Context(), orgID, userID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":               tp,
		"allow":              dec.Allow,
		"authorization_mode": dec.AuthorizationMode,
		"reasons":            dec.Reasons,
	})
}

// BatchGreenAutorunConfenge — POST /confenge/campaign/green-autorun/batch
func (h *Handler) BatchGreenAutorunConfenge(c *gin.Context) {
	orgID, ok := h.confengeOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var body struct {
		Limit int `json:"limit"`
	}
	_ = c.ShouldBindJSON(&body)
	queued, skipped, details, xerr := h.ConfengeService.RunGreenAutorunBatch(c.Request.Context(), orgID, userID, body.Limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"queued":  queued,
		"skipped": skipped,
		"details": details,
	})
}
