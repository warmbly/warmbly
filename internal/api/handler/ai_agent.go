// Dashboard AI agent endpoints. Sessions and their message runs are per-user;
// message and approval runs stream over SSE (text deltas, tool step events,
// approval_required, done with credits_remaining). The run executes in the
// request context, so a client that aborts the fetch cancels the run (the stop
// mechanism). Tools execute AS the invoking member with their org permission
// bits enforced by the registry.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aiagent"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// jwtInvocation builds a tool invocation for a JWT (dashboard) caller: it runs
// as the member with their org permission bits, never an API key.
func (h *Handler) jwtInvocation(c *gin.Context) (aitools.Invocation, *errx.Error) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return aitools.Invocation{}, errx.New(errx.BadRequest, "no organization selected")
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		return aitools.Invocation{}, errx.New(errx.Unauthorized, "invalid user")
	}
	member, xerr := h.OrganizationService.GetMembership(c.Request.Context(), *orgID, userID)
	if xerr != nil || member == nil {
		return aitools.Invocation{}, errx.New(errx.Forbidden, "not a member of this organization")
	}
	return aitools.Invocation{
		OrgID:     *orgID,
		UserID:    userID,
		OrgPerms:  member.Permissions,
		IsAPIKey:  false,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}, nil
}

// CreateAgentSession — POST /ai/sessions
func (h *Handler) CreateAgentSession(c *gin.Context) {
	if h.AIAgentService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the AI assistant is not configured"))
		return
	}
	inv, xerr := h.jwtInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	var req struct {
		Page     string `json:"page"`
		Resource string `json:"resource"`
	}
	_ = c.ShouldBindJSON(&req)

	sess, serr := h.AIAgentService.CreateSession(c.Request.Context(), inv.OrgID, inv.UserID, req.Page, req.Resource)
	if serr != nil {
		errx.JSON(c, serr)
		return
	}
	c.JSON(http.StatusOK, sess)
}

// ListAgentSessions — GET /ai/sessions (cursor paginated, newest first)
func (h *Handler) ListAgentSessions(c *gin.Context) {
	if h.AIAgentService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the AI assistant is not configured"))
		return
	}
	inv, xerr := h.jwtInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	limit, xerr := parseCreditLimit(c.Query("limit"), 25, 100)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	beforeAt, beforeID, xerr := paging.DecodeTimeCursor(c.Query("cursor"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	sessions, err := h.AIAgentService.ListSessions(c.Request.Context(), inv.OrgID, inv.UserID, limit+1, beforeAt, beforeID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list sessions"))
		return
	}
	var nextCursor *string
	if len(sessions) > limit {
		last := sessions[limit-1]
		nextCursor = paging.EncodeTime(last.CreatedAt, last.ID)
		sessions = sessions[:limit]
	}
	c.JSON(http.StatusOK, gin.H{
		"data":       sessions,
		"pagination": gin.H{"next_cursor": nextCursor, "has_more": nextCursor != nil},
	})
}

// AgentSessionMessages — GET /ai/sessions/:id/messages returns a session's
// hydrated transcript (+ any pending approval) so a reopened tab rehydrates.
func (h *Handler) AgentSessionMessages(c *gin.Context) {
	if h.AIAgentService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the AI assistant is not configured"))
		return
	}
	inv, xerr := h.jwtInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid session id"))
		return
	}
	sess, gerr := h.AIAgentService.GetSession(c.Request.Context(), inv.OrgID, inv.UserID, sessionID)
	if gerr != nil || sess == nil {
		errx.JSON(c, errx.New(errx.NotFound, "session not found"))
		return
	}
	turns, terr := h.AIAgentService.Transcript(c.Request.Context(), inv.OrgID, inv.UserID, sessionID)
	if terr != nil {
		errx.JSON(c, terr)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"title":      sess.Title,
		"turns":      turns,
		"pending":    sess.Context.Pending,
		"free_model": sess.Context.FreeModel,
	})
}

// AgentMessage — POST /ai/sessions/:id/messages (SSE)
func (h *Handler) AgentMessage(c *gin.Context) {
	if h.AIAgentService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the AI assistant is not configured"))
		return
	}
	inv, xerr := h.jwtInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid session id"))
		return
	}
	var req struct {
		MessageID string `json:"message_id"`
		Text      string `json:"text"`
		Page      string `json:"page"`
		Resource  string `json:"resource"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	if req.MessageID == "" {
		req.MessageID = uuid.NewString()
	}

	emit := sseEmitter(c)
	if serr := h.AIAgentService.RunMessage(c.Request.Context(), inv, sessionID, req.MessageID, req.Text, req.Page, req.Resource, emit); serr != nil {
		emit(aiagent.StreamEvent{Type: "error", Code: string(codeIdentifier(serr)), Message: serr.Message})
	}
}

// AgentApprove — POST /ai/sessions/:id/approve (SSE) resumes a paused run.
func (h *Handler) AgentApprove(c *gin.Context) {
	if h.AIAgentService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the AI assistant is not configured"))
		return
	}
	inv, xerr := h.jwtInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid session id"))
		return
	}
	var req struct {
		Decision string `json:"decision"` // approve | deny | always_allow
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	switch req.Decision {
	case "approve", "deny", "always_allow":
	default:
		errx.JSON(c, errx.New(errx.BadRequest, "decision must be approve, deny, or always_allow"))
		return
	}

	emit := sseEmitter(c)
	if serr := h.AIAgentService.Resume(c.Request.Context(), inv, sessionID, req.Decision, emit); serr != nil {
		emit(aiagent.StreamEvent{Type: "error", Code: string(codeIdentifier(serr)), Message: serr.Message})
	}
}

// sseEmitter prepares the response for Server-Sent Events and returns a
// flush-per-event emitter. Safe to call once per request.
func sseEmitter(c *gin.Context) func(aiagent.StreamEvent) {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable proxy buffering so deltas flush
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	return func(ev aiagent.StreamEvent) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// codeIdentifier gives a short machine code for an errx to surface in the SSE
// error event.
func codeIdentifier(e *errx.Error) string {
	switch e.Code {
	case errx.NotFound:
		return "not_found"
	case errx.BadRequest:
		return "bad_request"
	case errx.Forbidden:
		return "forbidden"
	case errx.ServiceUnavailable:
		return "service_unavailable"
	default:
		return "error"
	}
}
