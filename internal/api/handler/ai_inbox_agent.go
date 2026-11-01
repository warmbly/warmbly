// Inbox agent draft review (M10). The inbox agent drafts a suggested reply on an
// inbound human reply and persists it here awaiting a human decision. These
// endpoints let a person list pending drafts, approve-and-send one (through the
// normal reply path), or discard it. The agent never sends: approving is the
// only path that transmits.
package handler

import (
	"errors"
	"math"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
	"github.com/warmbly/warmbly/internal/repository"
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
	if len(restrictedMailboxes(c)) > 0 {
		kept := drafts[:0]
		for _, d := range drafts {
			if middleware.EmailAccountAllowed(c, d.EmailAccountID) {
				kept = append(kept, d)
			}
		}
		drafts = kept
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
	if xerr := mailboxAllowed(c, draft.EmailAccountID); xerr != nil {
		errx.Handle(c, xerr)
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
	if mailhdr.ValidMessageID(draft.InReplyTo) {
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
	if len(restrictedMailboxes(c)) > 0 {
		draft, derr := h.AIDraftRepo.GetDraft(c.Request.Context(), *orgID, draftID)
		if derr != nil {
			errx.Handle(c, errx.InternalError())
			return
		}
		if draft != nil {
			if xerr := mailboxAllowed(c, draft.EmailAccountID); xerr != nil {
				errx.Handle(c, xerr)
				return
			}
		}
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

const (
	maxThreadDraftBody   = 8000
	maxThreadDraftThread = 998
	maxThreadDraftField  = 40
)

// threadDraftSource is the label stored as the draft's model: a short slug naming the writer.
var threadDraftSource = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type upsertThreadDraftRequest struct {
	ThreadID    string   `json:"thread_id"`
	Body        string   `json:"body"`
	Source      string   `json:"source"`
	IntentClass string   `json:"intent_class"`
	Confidence  *float64 `json:"confidence"`
}

// UpsertThreadDraft — PUT /unibox/ai-drafts/by-thread
// Writes a reply draft for the newest inbound message on a thread: replaces the
// thread's pending draft or creates one, for review in the Drafts view. It never sends.
func (h *Handler) UpsertThreadDraft(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if !h.gateUnibox(c) {
		return
	}
	if h.AIDraftRepo == nil {
		errx.Handle(c, errx.New(errx.ServiceUnavailable, "the inbox agent is not configured"))
		return
	}
	var req upsertThreadDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	params, xerr := req.params()
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	// A mailbox-limited caller reaches only drafts in its own mailboxes.
	params.AllowedMailboxes = restrictedMailboxes(c)

	draft, created, err := h.AIDraftRepo.UpsertPendingDraftForThread(c.Request.Context(), *orgID, params)
	switch {
	case errors.Is(err, repository.ErrDraftThreadNotFound):
		errx.Handle(c, errx.NewWithIdentifier(errx.NotFound, "thread_not_found", "No inbound message on this thread."))
		return
	case errors.Is(err, repository.ErrDraftMailboxNotAllowed):
		errx.Handle(c, middleware.ErrEmailAccountNotAllowed(c))
		return
	case errors.Is(err, repository.ErrDraftAlreadySent):
		errx.Handle(c, errx.NewWithIdentifier(errx.Conflict, "already_sent", "The newest inbound message on this thread already has an approved reply."))
		return
	case errors.Is(err, repository.ErrDraftDiscarded):
		errx.Handle(c, errx.NewWithIdentifier(errx.Conflict, "draft_discarded", "The newest inbound message on this thread already has a discarded draft."))
		return
	case err != nil:
		log.Error().Err(err).Str("thread_id", params.ThreadID).Msg("inbox agent: thread draft upsert failed")
		errx.Handle(c, errx.InternalError())
		return
	}

	emailID := ""
	if draft.SourceMessageID != nil {
		emailID = draft.SourceMessageID.String()
	}
	h.StreamingPublisher.PublishAIDraftReady(c.Request.Context(), *orgID, uuid.Nil, draft.ThreadID, draft.ID.String(), emailID)
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityUnibox, &draft.EmailAccountID, nil, map[string]string{
		"source":    params.Model,
		"action":    "upsert_draft",
		"thread_id": draft.ThreadID,
	})
	c.JSON(http.StatusOK, gin.H{"id": draft.ID, "status": models.AIDraftPending, "created": created})
}

// params validates the body and trims it into repository params.
func (r upsertThreadDraftRequest) params() (repository.UpsertDraftParams, *errx.Error) {
	bad := func(msg string) (repository.UpsertDraftParams, *errx.Error) {
		return repository.UpsertDraftParams{}, errx.NewWithIdentifier(errx.BadRequest, "invalid_draft", msg)
	}
	thread := strings.TrimSpace(r.ThreadID)
	body := strings.TrimSpace(r.Body)
	source := strings.TrimSpace(r.Source)
	intent := strings.TrimSpace(r.IntentClass)
	switch {
	case thread == "" || utf8.RuneCountInString(thread) > maxThreadDraftThread || strings.ContainsRune(thread, 0):
		return bad("thread_id is required and at most 998 characters.")
	case body == "" || !utf8.ValidString(body) || strings.ContainsRune(body, 0) || utf8.RuneCountInString(body) > maxThreadDraftBody:
		return bad("body is required, valid text, and at most 8000 characters.")
	case source == "" || len(source) > maxThreadDraftField || !threadDraftSource.MatchString(source):
		return bad("source is required: letters, digits, dot, dash or underscore, at most 40 characters.")
	case utf8.RuneCountInString(intent) > maxThreadDraftField || !utf8.ValidString(intent) || strings.ContainsRune(intent, 0):
		return bad("intent_class is at most 40 characters.")
	}
	conf := 0.0
	if r.Confidence != nil {
		conf = *r.Confidence
		if math.IsNaN(conf) || conf < 0 || conf > 1 {
			return bad("confidence is a number from 0 to 1.")
		}
	}
	return repository.UpsertDraftParams{ThreadID: thread, Body: body, Model: source, IntentClass: intent, Confidence: conf}, nil
}
