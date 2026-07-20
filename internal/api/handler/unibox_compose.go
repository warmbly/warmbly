package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/compose"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// bareAddress extracts the address from a "Display Name <addr@x>" header
// entry; plain addresses pass through unchanged.
func bareAddress(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "<"); i != -1 {
		if j := strings.Index(s[i:], ">"); j != -1 {
			return strings.TrimSpace(s[i+1 : i+j])
		}
	}
	return s
}

type composeCandidateResponse struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	Provider      string     `json:"provider"`
	AuthState     string     `json:"auth_state"`
	WarmupActive  bool       `json:"warmup_active"`
	DailyLimit    int        `json:"daily_limit"`
	SentToday     int        `json:"sent_today"`
	RemainingDay  int        `json:"remaining_today"`
	History       int        `json:"history_messages"`
	LastContactAt *time.Time `json:"last_contact_at,omitempty"`
	Score         int        `json:"score"`
	Reasons       []string   `json:"reasons"`
	Recommended   bool       `json:"recommended"`
}

func toComposeCandidateResponse(c *compose.Candidate) composeCandidateResponse {
	reasons := c.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return composeCandidateResponse{
		ID:            c.Account.ID,
		Email:         c.Account.Email,
		Name:          c.Account.Name,
		Provider:      c.Account.Provider,
		AuthState:     c.Account.AuthState,
		WarmupActive:  c.Account.IsWarmingActive(),
		DailyLimit:    c.DailyLimit,
		SentToday:     c.SentToday,
		RemainingDay:  c.Remaining(),
		History:       c.History,
		LastContactAt: c.LastContactAt,
		Score:         c.Score,
		Reasons:       reasons,
		Recommended:   c.Recommended,
	}
}

// GetComposeCandidates powers the compose mailbox picker: every active
// mailbox scored against the recipient, the Auto recommendation with its
// reason, plus the resolved contact and suppression state for the address.
// GET /unibox/compose/candidates?to=<address>
func (h *Handler) GetComposeCandidates(c *gin.Context) {
	if !h.gateUnibox(c) {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	address := bareAddress(c.Query("to"))

	candidates, xerr := h.ComposeService.Candidates(c.Request.Context(), userID, *orgID, address)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	out := make([]composeCandidateResponse, len(candidates))
	var recommendedID *uuid.UUID
	var recommendedReason string
	for i := range candidates {
		out[i] = toComposeCandidateResponse(&candidates[i])
		if candidates[i].Recommended {
			id := candidates[i].Account.ID
			recommendedID = &id
			recommendedReason = compose.RecommendedReason(&candidates[i])
		}
	}

	resp := gin.H{
		"accounts":               out,
		"recommended_account_id": recommendedID,
		"recommended_reason":     recommendedReason,
		"contact":                nil,
		"suppression":            nil,
	}

	if address != "" {
		if contact, cerr := h.ContactService.GetByEmail(c.Request.Context(), orgID, address); cerr == nil && contact != nil {
			resp["contact"] = contact
		}
		if h.AdvancedService != nil {
			if suppressed, reason, serr := h.AdvancedService.ShouldSuppressRecipient(c.Request.Context(), *orgID, address); serr == nil && suppressed {
				resp["suppression"] = gin.H{"reason": reason}
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

type UniboxComposeRequest struct {
	// EmailAccountID empty (or "auto") lets the backend pick the best
	// mailbox for the first recipient.
	EmailAccountID string `json:"email_account_id"`
	// FromTagID scopes the automatic pick to mailboxes carrying this tag
	// ("Auto within a tag"). Ignored when EmailAccountID is explicit.
	FromTagID   string     `json:"from_tag_id"`
	To          []string   `json:"to" binding:"required,min=1"`
	CC          []string   `json:"cc"`
	BCC         []string   `json:"bcc"`
	Subject     string     `json:"subject" binding:"required"`
	BodyHTML    string     `json:"body_html"`
	BodyPlain   string     `json:"body_plain"`
	SendMode    string     `json:"send_mode"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
}

// UniboxCompose sends a brand-new outbound email (not a reply). Unlike the
// reply path it enforces org-wide recipient suppression before queueing, and
// supports auto mailbox selection.
// POST /unibox/compose
func (h *Handler) UniboxCompose(c *gin.Context) {
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

	var req UniboxComposeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	// Suppression gate: a recipient who bounced, complained, or
	// unsubscribed org-wide must not be reachable through compose.
	if h.AdvancedService != nil {
		all := make([]string, 0, len(req.To)+len(req.CC)+len(req.BCC))
		all = append(all, req.To...)
		all = append(all, req.CC...)
		all = append(all, req.BCC...)
		for _, rcpt := range all {
			addr := bareAddress(rcpt)
			if addr == "" {
				continue
			}
			suppressed, reason, serr := h.AdvancedService.ShouldSuppressRecipient(c.Request.Context(), *orgID, addr)
			if serr != nil {
				errx.Handle(c, serr)
				return
			}
			if suppressed {
				msg := addr + " is suppressed for this workspace"
				if reason != "" {
					msg += " (" + reason + ")"
				}
				errx.Handle(c, errx.New(errx.BadRequest, msg))
				return
			}
		}
	}

	var accountID *uuid.UUID
	if v := strings.TrimSpace(req.EmailAccountID); v != "" && v != "auto" {
		id, perr := uuid.Parse(v)
		if perr != nil {
			errx.Handle(c, errx.ErrUuid)
			return
		}
		accountID = &id
	}

	var tagID *uuid.UUID
	if accountID == nil {
		if v := strings.TrimSpace(req.FromTagID); v != "" {
			id, perr := uuid.Parse(v)
			if perr != nil {
				errx.Handle(c, errx.ErrUuid)
				return
			}
			tagID = &id
		}
	}

	candidate, auto, xerr := h.ComposeService.Resolve(c.Request.Context(), userID, *orgID, accountID, tagID, bareAddress(req.To[0]))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	sendReq := &emailsend.SendEmailRequest{
		To:          req.To,
		CC:          req.CC,
		BCC:         req.BCC,
		Subject:     req.Subject,
		BodyHTML:    req.BodyHTML,
		BodyPlain:   req.BodyPlain,
		SendMode:    req.SendMode,
		ScheduledAt: req.ScheduledAt,
	}
	if sendReq.SendMode == "" {
		sendReq.SendMode = "instant"
	}

	resp, xerr := h.EmailSendService.SendEmail(c.Request.Context(), userID, *orgID, candidate.Account.ID, sendReq)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	chosenID := candidate.Account.ID
	h.auditOrg(c, models.AuditActionSend, models.AuditEntityUnibox, &chosenID, nil, map[string]string{
		"compose":    "true",
		"auto":       strconv.FormatBool(auto),
		"send_mode":  sendReq.SendMode,
		"recipients": strconv.Itoa(len(req.To)),
	})

	out := gin.H{
		"task_id":       resp.TaskID,
		"scheduled_at":  resp.ScheduledAt,
		"send_mode":     resp.SendMode,
		"account_id":    candidate.Account.ID,
		"account_email": candidate.Account.Email,
		"auto":          auto,
	}
	if auto {
		out["picked_reason"] = compose.RecommendedReason(candidate)
	}
	c.JSON(http.StatusOK, out)
}
