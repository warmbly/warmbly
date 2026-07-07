package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// gateUnibox enforces feature access for any unibox endpoint that
// needs an org context. Returns true when the caller is allowed.
func (h *Handler) gateUnibox(c *gin.Context) bool {
	if h.FeatureGateService == nil {
		return true
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return true
	}
	canUse, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)
	if !canUse {
		errx.Handle(c, errx.New(errx.Forbidden, "Unibox requires an active trial or paid subscription"))
		return false
	}
	return true
}

func (h *Handler) GetUniboxIncoming(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	// Check if organization can use unibox (active free trial or paid subscription)
	if h.FeatureGateService != nil {
		orgID := middleware.GetOrganizationID(c)
		if orgID != nil {
			canUse, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)
			if !canUse {
				errx.Handle(c, errx.New(errx.Forbidden, "Unibox requires an active trial or paid subscription"))
				return
			}
		}
	}

	// Build search params from query
	params := &models.MailSearchParams{
		Cursor: c.Query("cursor"),
	}

	// Parse limit
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			params.PageSize = l
		}
	}

	// Parse sender filter
	if from := c.Query("from"); from != "" {
		params.Sender = &from
	}

	// Parse subject filter
	if subject := c.Query("subject"); subject != "" {
		params.Subject = &subject
	}

	// Parse unseen filter
	if unseenStr := c.Query("unseen"); unseenStr == "true" {
		unseen := true
		params.Unseen = &unseen
	}

	// Snoozed scope. snoozed=true → only snoozed; snoozed=any → no
	// filter; absent → default behaviour (exclude snoozed). The "any"
	// value is supplied by admin debug tooling, not the dashboard.
	switch c.Query("snoozed") {
	case "true":
		v := true
		params.Snoozed = &v
	case "any":
		v := false
		params.Snoozed = &v
	}

	if c.Query("awaiting_reply") == "true" {
		v := true
		params.AwaitingReply = &v
	}

	// Parse date filters
	if sinceStr := c.Query("since"); sinceStr != "" {
		if since, err := time.Parse("2006-01-02", sinceStr); err == nil {
			params.Since = &since
		}
	}
	if untilStr := c.Query("until"); untilStr != "" {
		if until, err := time.Parse("2006-01-02", untilStr); err == nil {
			params.Until = &until
		}
	}

	// Parse email account filter. Accepts either:
	//   - email_id=<uuid>          single account (legacy)
	//   - email_ids=<uuid>,<uuid>  comma-separated list (used by the
	//                              tag/multi-account filter sheet)
	// Invalid UUIDs are silently dropped; an empty resulting list
	// behaves the same as "no account filter".
	collectAccountIDs := func(raw string) {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if id, err := uuid.Parse(s); err == nil {
				params.EmailAccountIDs = append(params.EmailAccountIDs, id)
			}
		}
	}
	if v := c.Query("email_id"); v != "" {
		collectAccountIDs(v)
	}
	if v := c.Query("email_ids"); v != "" {
		collectAccountIDs(v)
	}

	// Uncategorized: threads carrying no conversation labels at all.
	if c.Query("uncategorized") == "true" {
		v := true
		params.Uncategorized = &v
	}

	// Conversation-label filter: category_ids=<uuid>,<uuid>. A thread
	// matches if it carries any of the listed labels. Invalid UUIDs are
	// dropped, matching the email_ids behaviour.
	if v := c.Query("category_ids"); v != "" {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if id, err := uuid.Parse(s); err == nil {
				params.CategoryIDs = append(params.CategoryIDs, id)
			}
		}
	}

	resp, xerr := h.UniboxService.Search(c.Request.Context(), *orgID, uid, params)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetUniboxEmail(c *gin.Context) {
	// Org-scoped: the inbox list is org-wide, so opening a message must be too.
	// A non-owner member who sees a message in the org-scoped list would
	// otherwise get "email not found" because the row is keyed to the mailbox
	// owner's user_id, not theirs.
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	// Check if organization can use unibox
	if h.FeatureGateService != nil {
		canUse, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)
		if !canUse {
			errx.Handle(c, errx.New(errx.Forbidden, "Unibox requires an active trial or paid subscription"))
			return
		}
	}

	mailID := c.Param("id")
	mid, err := uuid.Parse(mailID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	resp, xerr := h.UniboxService.GetByID(
		c.Request.Context(),
		*orgID, mid,
	)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetUniboxThread(c *gin.Context) {
	// Org-scoped: the inbox list is org-wide, so the thread view must be too.
	// Otherwise a non-owner member sees the conversation in the list but an
	// empty thread when they open it — the messages are keyed to the mailbox
	// owner's user_id, not theirs.
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	// Check if organization can use unibox
	if h.FeatureGateService != nil {
		canUse, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)
		if !canUse {
			errx.Handle(c, errx.New(errx.Forbidden, "Unibox requires an active trial or paid subscription"))
			return
		}
	}

	// email_id is optional. When omitted, the thread is read across every
	// mailbox in the organization — the natural unified-inbox view where the
	// caller only knows the thread.
	var eid uuid.UUID
	emailID := c.Query("email")
	if emailID == "" {
		emailID = c.Query("email_id")
	}
	if emailID != "" {
		parsed, err := uuid.Parse(emailID)
		if err != nil {
			errx.Handle(c, errx.ErrUuid)
			return
		}
		eid = parsed
	}

	threadID := c.Query("id")
	if threadID == "" {
		threadID = c.Query("thread_id")
	}
	if threadID == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "thread_id is required"))
		return
	}
	cursor := c.Query("cursor")
	limit := c.Query("limit")

	resp, xerr := h.UniboxService.GetByThread(
		c.Request.Context(),
		*orgID, eid,
		threadID, limit, cursor,
	)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUniboxThreadLabels returns the conversation labels on a thread.
// GET /unibox/thread/labels?thread_id=<id>
func (h *Handler) GetUniboxThreadLabels(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	threadID := c.Query("thread_id")
	if threadID == "" {
		threadID = c.Query("id")
	}
	if threadID == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "thread_id is required"))
		return
	}

	labels, xerr := h.UniboxService.ListThreadLabels(c.Request.Context(), uid, threadID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": labels})
}

// SetUniboxThreadLabels replaces the full conversation-label set on a
// thread. Idempotent (PUT semantics): the body's category_ids is the
// desired set, so retries are naturally safe. Only the user's own
// categories are attached.
// PUT /unibox/thread/labels
func (h *Handler) SetUniboxThreadLabels(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var req models.UniboxThreadLabels
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	labels, xerr := h.UniboxService.SetThreadLabels(c.Request.Context(), uid, req.ThreadID, req.CategoryIDs)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityUnibox, nil, nil, map[string]string{
		"action":    "set_labels",
		"thread_id": req.ThreadID,
		"labels":    strconv.Itoa(len(labels)),
	})

	c.JSON(http.StatusOK, gin.H{"data": labels})
}

func (h *Handler) UniboxMarkSeen(c *gin.Context) {
	// Org-scoped: the inbox + unread count are org-wide, so marking read must be
	// too, otherwise a non-owner member can never clear the shared unread badge.
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var data models.MarkSeen
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, xerr := h.UniboxService.MarkSeenBulk(c.Request.Context(), *orgID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUnseenCount gets the count of unseen emails
// GET /unibox/count
func (h *Handler) GetUnseenCount(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	// Optional email account filter
	var emailAccountID *uuid.UUID
	if emailIDStr := c.Query("email_id"); emailIDStr != "" {
		if id, err := uuid.Parse(emailIDStr); err == nil {
			emailAccountID = &id
		}
	}

	count, xerr := h.UniboxService.GetUnseenCount(c.Request.Context(), *orgID, emailAccountID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

type UniboxReplyRequest struct {
	EmailAccountID string     `json:"email_account_id" binding:"required"`
	To             []string   `json:"to" binding:"required,min=1"`
	CC             []string   `json:"cc"`
	BCC            []string   `json:"bcc"`
	Subject        string     `json:"subject" binding:"required"`
	BodyHTML       string     `json:"body_html"`
	BodyPlain      string     `json:"body_plain"`
	InReplyTo      []string   `json:"in_reply_to"`
	ThreadID       string     `json:"thread_id"`
	SendMode       string     `json:"send_mode"`
	ScheduledAt    *time.Time `json:"scheduled_at,omitempty"`
}

// UniboxReply schedules a reply email from Unibox.
// POST /unibox/reply
func (h *Handler) UniboxReply(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var req UniboxReplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	accountID, err := uuid.Parse(req.EmailAccountID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	sendReq := &emailsend.SendEmailRequest{
		To:          req.To,
		CC:          req.CC,
		BCC:         req.BCC,
		Subject:     req.Subject,
		BodyHTML:    req.BodyHTML,
		BodyPlain:   req.BodyPlain,
		InReplyTo:   req.InReplyTo,
		ThreadID:    req.ThreadID,
		SendMode:    req.SendMode,
		ScheduledAt: req.ScheduledAt,
	}
	if sendReq.SendMode == "" {
		sendReq.SendMode = "instant"
	}

	resp, xerr := h.EmailSendService.SendEmail(c.Request.Context(), userID, *orgID, accountID, sendReq)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionSend, models.AuditEntityUnibox, &accountID, nil, map[string]string{
		"send_mode":  sendReq.SendMode,
		"recipients": strconv.Itoa(len(req.To)),
	})

	c.JSON(http.StatusOK, resp)
}

// GetUniboxOverview rolls up unread/today/week/snoozed/awaiting plus
// per-mailbox and per-tag counts in one call. The dashboard's scope
// rail and metric strip share this response.
// GET /unibox/overview
func (h *Handler) GetUniboxOverview(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	resp, xerr := h.UniboxService.Overview(c.Request.Context(), *orgID, uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, resp)
}

type UniboxSnoozeRequest struct {
	ThreadID     string    `json:"thread_id" binding:"required"`
	SnoozedUntil time.Time `json:"snoozed_until" binding:"required"`
}

// CreateUniboxSnooze hides a thread until snoozed_until passes.
// Upsert semantics: a second call on the same thread updates the time.
// POST /unibox/snooze
func (h *Handler) CreateUniboxSnooze(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var req UniboxSnoozeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, xerr := h.UniboxService.Snooze(c.Request.Context(), uid, req.ThreadID, req.SnoozedUntil)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityUnibox, nil, nil, map[string]string{
		"action":    "snooze",
		"thread_id": req.ThreadID,
	})

	c.JSON(http.StatusOK, resp)
}

// DeleteUniboxSnooze un-snoozes a thread immediately. Idempotent —
// deleting a snooze that doesn't exist still returns 204.
// DELETE /unibox/snooze
func (h *Handler) DeleteUniboxSnooze(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	threadID := c.Query("thread_id")
	if threadID == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "thread_id is required"))
		return
	}

	if xerr := h.UniboxService.Unsnooze(c.Request.Context(), uid, threadID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityUnibox, nil, nil, map[string]string{
		"action":    "snooze",
		"thread_id": threadID,
	})

	c.Status(http.StatusNoContent)
}

// ListUniboxScheduled returns every pending email task the user has
// queued: what the "Scheduled" scope in the dashboard reads from.
// When `thread_id` is set we scope the response to a single thread,
// which the ThreadView uses to render queued replies inline. The
// response shape is identical either way so the same client + hook
// handle both forms.
// GET /unibox/scheduled
// GET /unibox/scheduled?thread_id=<id>
func (h *Handler) ListUniboxScheduled(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	if threadID := c.Query("thread_id"); threadID != "" {
		items, xerr := h.UniboxService.ListScheduledByThread(c.Request.Context(), uid, threadID)
		if xerr != nil {
			errx.Handle(c, xerr)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": items})
		return
	}

	items, xerr := h.UniboxService.ListScheduled(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// CancelUniboxScheduled cancels a pending scheduled send. DB-only:
// we mark the task cancelled and let the queued Cloud Task fire as a
// no-op (the task handler short-circuits on non-pending statuses).
// DELETE /unibox/scheduled/:task_id
func (h *Handler) CancelUniboxScheduled(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	taskID, err := uuid.Parse(c.Param("task_id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.UniboxService.CancelScheduled(c.Request.Context(), uid, taskID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityUnibox, &taskID, nil, map[string]string{
		"action": "cancel_scheduled",
	})

	c.Status(http.StatusNoContent)
}

// ListUniboxSnoozes returns the user's active snoozes (debug + UI).
// GET /unibox/snoozes
func (h *Handler) ListUniboxSnoozes(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	resp, xerr := h.UniboxService.ListSnoozes(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}
