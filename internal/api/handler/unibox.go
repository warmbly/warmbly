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

func (h *Handler) GetUniboxIncoming(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	// Check if organization can use unibox (paid subscription required)
	if h.FeatureGateService != nil {
		orgID := middleware.GetOrganizationID(c)
		if orgID != nil {
			canUse, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)
			if !canUse {
				errx.Handle(c, errx.New(errx.Forbidden, "Unibox requires a paid subscription"))
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

	resp, xerr := h.UniboxService.Search(c.Request.Context(), uid, params)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetUniboxEmail(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	// Check if organization can use unibox
	if h.FeatureGateService != nil {
		orgID := middleware.GetOrganizationID(c)
		if orgID != nil {
			canUse, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)
			if !canUse {
				errx.Handle(c, errx.New(errx.Forbidden, "Unibox requires a paid subscription"))
				return
			}
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
		uid, mid,
	)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetUniboxThread(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	// Check if organization can use unibox
	if h.FeatureGateService != nil {
		orgID := middleware.GetOrganizationID(c)
		if orgID != nil {
			canUse, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)
			if !canUse {
				errx.Handle(c, errx.New(errx.Forbidden, "Unibox requires a paid subscription"))
				return
			}
		}
	}

	emailID := c.Query("email")
	if emailID == "" {
		emailID = c.Query("email_id")
	}
	eid, err := uuid.Parse(emailID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	threadID := c.Query("id")
	if threadID == "" {
		threadID = c.Query("thread_id")
	}
	cursor := c.Query("cursor")
	limit := c.Query("limit")

	resp, xerr := h.UniboxService.GetByThread(
		c.Request.Context(),
		uid, eid,
		threadID, limit, cursor,
	)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UniboxMarkSeen(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var data models.MarkSeen
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, xerr := h.UniboxService.MarkSeenBulk(c.Request.Context(), uid, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUnseenCount gets the count of unseen emails
// GET /unibox/count
func (h *Handler) GetUnseenCount(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	// Optional email account filter
	var emailAccountID *uuid.UUID
	if emailIDStr := c.Query("email_id"); emailIDStr != "" {
		if id, err := uuid.Parse(emailIDStr); err == nil {
			emailAccountID = &id
		}
	}

	count, xerr := h.UniboxService.GetUnseenCount(c.Request.Context(), uid, emailAccountID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

type UniboxReplyRequest struct {
	EmailAccountID string   `json:"email_account_id" binding:"required"`
	To             []string `json:"to" binding:"required,min=1"`
	CC             []string `json:"cc"`
	BCC            []string `json:"bcc"`
	Subject        string   `json:"subject" binding:"required"`
	BodyHTML       string   `json:"body_html"`
	BodyPlain      string   `json:"body_plain"`
	InReplyTo      []string `json:"in_reply_to"`
	ThreadID       string   `json:"thread_id"`
	SendMode       string   `json:"send_mode"`
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
		To:        req.To,
		CC:        req.CC,
		BCC:       req.BCC,
		Subject:   req.Subject,
		BodyHTML:  req.BodyHTML,
		BodyPlain: req.BodyPlain,
		InReplyTo: req.InReplyTo,
		ThreadID:  req.ThreadID,
		SendMode:  req.SendMode,
	}
	if sendReq.SendMode == "" {
		sendReq.SendMode = "instant"
	}

	resp, xerr := h.EmailSendService.SendEmail(c.Request.Context(), userID, *orgID, accountID, sendReq)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}
