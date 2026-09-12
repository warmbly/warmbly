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

func (h *Handler) EmailsSearch(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	query := c.Query("q")
	cursor := c.Query("cursor")
	tag := c.Query("tag")
	limit := c.Query("limit")

	resp, err := h.EmailService.Search(c.Request.Context(), orgID.String(), query, cursor, tag, limit, middleware.GetAPIKeyAllowedEmailAccounts(c))
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetEmail(c *gin.Context) {
	// Mailboxes are workspace assets and the query behind this is scoped by
	// organization; passing the caller's user id here made the detail endpoint
	// 404 for everyone, including the owner who connected the mailbox.
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	emailAccountID := c.Param("id")

	resp, err := h.EmailService.Get(c.Request.Context(), orgID.String(), emailAccountID)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateEmail(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	userIDStr := middleware.GetUserID(c)

	emailAccountID := c.Param("id")

	var data models.UpdateEmail

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.EmailService.Update(c.Request.Context(), orgID.String(), userIDStr, emailAccountID, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if accountID, err := uuid.Parse(emailAccountID); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, nil, nil)
	}

	c.JSON(http.StatusOK, resp)
}

// BulkTagEmails adds/removes tags across many mailboxes in one call — the
// mailboxes list bulk bar. Naturally idempotent (set semantics), so retries
// are safe without an Idempotency-Key.
// PATCH /emails/tags
func (h *Handler) BulkTagEmails(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

	var data models.BulkEmailTags
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	parse := func(raw []string) ([]uuid.UUID, bool) {
		out := make([]uuid.UUID, 0, len(raw))
		seen := make(map[uuid.UUID]struct{}, len(raw))
		for _, s := range raw {
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, false
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		return out, true
	}

	emailIDs, ok := parse(data.EmailIDs)
	if !ok {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	// The mailbox scope here is the workspace, so a restricted API key needs
	// the same allowlist check the per-id routes get from
	// RequireAPIKeyEmailAccountParam; there is no path param to gate on.
	for _, id := range emailIDs {
		if !middleware.APIKeyAllowsEmailAccount(c, id) {
			errx.Handle(c, errx.New(errx.Forbidden, "email account is not allowed for this API key"))
			return
		}
	}
	addTags, ok := parse(data.AddTags)
	if !ok {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	removeTags, ok := parse(data.RemoveTags)
	if !ok {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	updated, err := h.EmailService.BulkUpdateTags(c.Request.Context(), orgID.String(), emailIDs, addTags, removeTags)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, nil, nil, map[string]string{
		"bulk_tags": "true",
		"accounts":  strconv.Itoa(updated),
		"added":     strconv.Itoa(len(addTags)),
		"removed":   strconv.Itoa(len(removeTags)),
	})

	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

// StartWarmup enables (or resumes) warmup for a mailbox, preserving ramp
// progress when resuming from a paused state.
func (h *Handler) StartWarmup(c *gin.Context) { h.warmupLifecycle(c, "start") }

// PauseWarmup pauses warmup without losing ramp progress; a later start
// continues from the same daily volume.
func (h *Handler) PauseWarmup(c *gin.Context) { h.warmupLifecycle(c, "pause") }

// ResumeWarmup resumes a paused warmup, shifting the ramp anchor forward so
// progress continues where it left off.
func (h *Handler) ResumeWarmup(c *gin.Context) { h.warmupLifecycle(c, "resume") }

// StopWarmup disables warmup entirely and clears ramp progress — a later
// start begins a fresh ramp. Distinct from PauseWarmup, which preserves
// progress.
func (h *Handler) StopWarmup(c *gin.Context) { h.warmupLifecycle(c, "stop") }

func (h *Handler) warmupLifecycle(c *gin.Context, action string) {
	userIDStr := middleware.GetUserID(c)
	emailAccountID := c.Param("id")

	resp, err := h.EmailService.SetWarmupLifecycle(c.Request.Context(), userIDStr, emailAccountID, action)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Seed/repair the warmup chain immediately so warming starts now rather
	// than on the next reconciler pass. Best-effort: the reconciler is the
	// backstop, and pause has nothing to seed.
	if action == "start" || action == "resume" {
		if accountID, perr := uuid.Parse(emailAccountID); perr == nil {
			_ = h.TasksService.EnsureWarmupScheduled(c.Request.Context(), accountID)
		}
	}

	// Audit log
	if accountID, err := uuid.Parse(emailAccountID); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, map[string]string{"warmup": action}, nil)
	}

	c.JSON(http.StatusOK, resp)
}

// HoldEmail serves POST /emails/:id/hold: the owner takes the mailbox out of
// campaign sending until they release it. Warmup is untouched.
func (h *Handler) HoldEmail(c *gin.Context) { h.sendHold(c, true) }

// ReleaseEmail serves POST /emails/:id/release, putting a held or resting
// mailbox back into campaign rotation.
func (h *Handler) ReleaseEmail(c *gin.Context) { h.sendHold(c, false) }

func (h *Handler) sendHold(c *gin.Context, hold bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	emailAccountID := c.Param("id")

	state, err := h.EmailService.SetSendHold(c.Request.Context(), orgID.String(), emailAccountID, hold)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	action := "released"
	if hold {
		action = "held"
	}
	if accountID, perr := uuid.Parse(emailAccountID); perr == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, map[string]string{"send_hold": action}, nil)
	}

	c.JSON(http.StatusOK, state)
}

// GetEmailTrackingDomain serves GET /emails/:id/track: the mailbox's stored
// tracking-domain state plus the CNAME target this install expects. Read-only,
// so it does no DNS work; VerifyEmailTrackingDomain is the live check.
func (h *Handler) GetEmailTrackingDomain(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	status, err := h.EmailService.GetTrackingDomain(c.Request.Context(), orgID.String(), c.Param("id"))
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, status)
}

// VerifyEmailTrackingDomain serves POST /emails/:id/track/verify: re-resolves
// the stored domain and persists the verdict. Write-scoped, because recording
// it is what routes real links through the custom host.
func (h *Handler) VerifyEmailTrackingDomain(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	status, err := h.EmailService.VerifyTrackingDomain(c.Request.Context(), orgID.String(), c.Param("id"))
	if err != nil {
		errx.Handle(c, err)
		return
	}

	if accountID, perr := uuid.Parse(c.Param("id")); perr == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID,
			map[string]string{"tracking_domain_verified": strconv.FormatBool(status.TrackingDomainVerified)}, nil)
	}

	c.JSON(http.StatusOK, status)
}

func (h *Handler) UpdateEmailTrackingDomain(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	emailAccountID := c.Param("id")
	domain := c.Query("domain")

	status, err := h.EmailService.UpdateTrackingDomain(c.Request.Context(), orgID.String(), emailAccountID, domain)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if accountID, err := uuid.Parse(emailAccountID); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, map[string]string{"tracking_domain": domain}, nil)
	}

	c.JSON(http.StatusOK, status)
}

func (h *Handler) DeleteEmail(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	emailAccountID := c.Param("id")

	if err := h.EmailService.Delete(c.Request.Context(), userIDStr, emailAccountID); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if accountID, err := uuid.Parse(emailAccountID); err == nil {
		h.auditOrg(c, models.AuditActionDelete, models.AuditEntityEmailAccount, &accountID, nil, nil)
	}

	c.Status(http.StatusNoContent)
}
