// Inbox agent draft review (M10). The inbox agent drafts a suggested reply on an
// inbound human reply and persists it here awaiting a human decision. These
// endpoints let a person list pending drafts, approve-and-send one (through the
// normal reply path), or discard it. The agent never sends: approving is the
// only path that transmits.
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ListAgentDrafts — GET /unibox/agent-drafts
// Returns the org's pending inbox-agent drafts, newest first.
func (h *Handler) ListAgentDrafts(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.gateUnibox(c) {
		return
	}
	if h.AIDraftRepo == nil {
		c.JSON(http.StatusOK, gin.H{"data": []models.AIThreadDraft{}})
		return
	}
	drafts, err := h.AIDraftRepo.ListPendingDrafts(c.Request.Context(), *orgID, 100)
	if err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": drafts})
}

// ApproveAgentDraft — POST /unibox/agent-drafts/:id/approve
// Sends the draft (optionally with an edited body) through the normal reply path
// and marks it approved. The status flip is claimed BEFORE sending so two
// approvals can never double-send.
func (h *Handler) ApproveAgentDraft(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	if !h.gateUnibox(c) {
		return
	}
	draftID, perr := uuid.Parse(c.Param("id"))
	if perr != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	if h.AIDraftRepo == nil {
		errx.Handle(c, errx.New(errx.ServiceUnavailable, "the inbox agent is not configured"))
		return
	}

	// Optional edited body (the Edit-then-send path). Empty keeps the draft body.
	var req struct {
		Body string `json:"body"`
	}
	_ = c.ShouldBindJSON(&req)

	draft, derr := h.AIDraftRepo.GetDraft(c.Request.Context(), *orgID, draftID)
	if derr != nil {
		errx.Handle(c, errx.InternalError())
		return
	}
	if draft == nil {
		errx.Handle(c, errx.New(errx.NotFound, "draft not found"))
		return
	}
	if draft.Status != models.AIDraftPending {
		errx.Handle(c, errx.New(errx.Conflict, "this draft was already handled"))
		return
	}

	// Claim it: only the first approver flips pending -> approved and proceeds.
	claimed, cerr := h.AIDraftRepo.SetDraftStatus(c.Request.Context(), *orgID, draftID, models.AIDraftApproved)
	if cerr != nil {
		errx.Handle(c, errx.InternalError())
		return
	}
	if !claimed {
		errx.Handle(c, errx.New(errx.Conflict, "this draft was already handled"))
		return
	}

	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = draft.Body
	}
	sendReq := &emailsend.SendEmailRequest{
		To:        []string{draft.ToAddr},
		Subject:   draft.Subject,
		BodyPlain: body,
		ThreadID:  draft.ThreadID,
		SendMode:  "instant",
	}
	if draft.InReplyTo != "" {
		sendReq.InReplyTo = []string{draft.InReplyTo}
	}

	resp, xerr := h.EmailSendService.SendEmail(c.Request.Context(), userID, *orgID, draft.EmailAccountID, sendReq)
	if xerr != nil {
		// Send failed after we claimed the draft (pending -> approved). Return it
		// to pending so the human can retry rather than silently losing it. The
		// draft is now in the 'approved' state, so this needs the approved->pending
		// revert (SetDraftStatus only transitions FROM pending). If the revert
		// itself fails, the draft is stuck 'approved' and invisible to review — log
		// it so that is observable rather than a silent loss.
		if reverted, rerr := h.AIDraftRepo.RevertApprovedToPending(c.Request.Context(), *orgID, draftID); rerr != nil || !reverted {
			log.Error().Err(rerr).Str("draft_id", draftID.String()).Bool("reverted", reverted).Msg("inbox agent: failed to revert draft to pending after send failure")
		}
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionSend, models.AuditEntityUnibox, &draft.EmailAccountID, nil, map[string]string{
		"source":    "inbox_agent",
		"thread_id": draft.ThreadID,
	})

	c.JSON(http.StatusOK, resp)
}

// DiscardAgentDraft — POST /unibox/agent-drafts/:id/discard
// Dismisses a pending draft without sending.
func (h *Handler) DiscardAgentDraft(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.gateUnibox(c) {
		return
	}
	draftID, perr := uuid.Parse(c.Param("id"))
	if perr != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	if h.AIDraftRepo == nil {
		errx.Handle(c, errx.New(errx.ServiceUnavailable, "the inbox agent is not configured"))
		return
	}
	ok, err := h.AIDraftRepo.SetDraftStatus(c.Request.Context(), *orgID, draftID, models.AIDraftDiscarded)
	if err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}
	if !ok {
		errx.Handle(c, errx.New(errx.NotFound, "draft not found or already handled"))
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityUnibox, nil, nil, map[string]string{
		"source": "inbox_agent",
		"action": "discard_draft",
	})
	c.JSON(http.StatusOK, gin.H{"status": "discarded"})
}
