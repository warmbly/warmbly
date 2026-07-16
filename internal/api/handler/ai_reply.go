// AI reply draft for the unibox composer. Assembles the thread history, the
// counterpart contact (with custom fields and campaign membership), and the org
// voice profile into a context-grounded prompt, charges 2 credits, and returns
// a draft the human reviews and sends. It never sends anything itself.
package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func isInsufficientCredits(err error) bool { return errors.Is(err, credits.ErrInsufficientCredits) }
func isCapExceeded(err error) bool         { return errors.Is(err, credits.ErrCapExceeded) }

// DraftReply — POST /unibox/reply/draft
func (h *Handler) DraftReply(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}
	if h.AIProvider == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the AI assistant is not configured"))
		return
	}

	// Unibox entitlement + AI credits gate the feature.
	if allowed, xerr := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID); xerr != nil {
		errx.JSON(c, xerr)
		return
	} else if !allowed {
		errx.JSON(c, errx.New(errx.Forbidden, "The unified inbox requires an active trial or paid subscription."))
		return
	}

	var req struct {
		ThreadID    string `json:"thread_id" binding:"required"`
		Instruction string `json:"instruction"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	// Assemble thread context.
	thread, xerr := h.UniboxService.GetByThread(c.Request.Context(), *orgID, uuid.Nil, req.ThreadID, "20", "")
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if thread == nil || len(thread.Data) == 0 {
		errx.JSON(c, errx.New(errx.NotFound, "thread not found"))
		return
	}
	history, counterpart := h.buildThreadContext(thread.Data)

	// Look up the counterpart contact for grounding (best-effort).
	contactCtx := h.contactContext(c, userID, *orgID, counterpart)

	// Model tier + voice.
	paid, _ := h.FeatureGateService.IsPaidOrganization(c.Request.Context(), *orgID)
	model := h.AIProvider.ModelForTier(paid)
	voice := h.orgVoice(c.Request.Context(), *orgID, "")

	// Charge 2 credits up front (idempotent on the client's key); refund on
	// provider failure. A free/local model (AI_FREE) runs un-metered.
	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	local := h.AIProvider != nil && h.AIProvider.IsLocal()
	var remaining int
	if local {
		if bal, berr := h.CreditService.GetBalance(c.Request.Context(), *orgID); berr == nil {
			remaining = bal
		}
	} else {
		var cerr error
		remaining, cerr = h.CreditService.Consume(c.Request.Context(), *orgID, credits.CostReplyDraft, "reply_draft", model, 0, idemKey)
		if cerr != nil {
			mapCreditError(c, cerr)
			return
		}
	}

	system := generation.BuildReplyRules(voice)
	if h.SkillsService != nil {
		if pre := h.SkillsService.EnabledPreamble(c.Request.Context(), *orgID); pre != "" {
			system += "\n\n" + pre
		}
	}
	prompt := buildReplyPrompt(history, contactCtx, req.Instruction)
	result, gerr := h.AIProvider.Complete(c.Request.Context(), generation.CompletionRequest{
		System: system,
		Prompt: prompt,
		Model:  model,
	})
	if gerr != nil {
		if !local {
			if bal, rerr := h.CreditService.Grant(c.Request.Context(), *orgID, credits.CostReplyDraft, "reply_draft_refund"); rerr == nil {
				remaining = bal
			}
		}
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "The reply drafter is temporarily unavailable. Your credits were not charged."))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"text":              result.Text,
		"credits_remaining": remaining,
		"model":             result.Model,
	})
}

// buildThreadContext renders the thread's messages oldest-first and returns the
// counterpart email (the most recent sender) to look up as a contact.
func (h *Handler) buildThreadContext(msgs []models.EmailMessageStoreDataPreview) (string, string) {
	// GetByThread returns oldest-first, so render in order for a natural
	// transcript and take the counterpart from the most recent (last) message.
	var b strings.Builder
	counterpart := ""
	for _, m := range msgs {
		from := strings.Join(m.FromAddr, ", ")
		fmt.Fprintf(&b, "From: %s\nSubject: %s\n%s\n\n", from, m.Subject, strings.TrimSpace(m.Snippet))
	}
	if last := msgs[len(msgs)-1]; len(last.FromAddr) > 0 {
		counterpart = last.FromAddr[0]
	}
	return strings.TrimSpace(b.String()), counterpart
}

// contactContext returns a compact grounding block for the counterpart contact,
// or "" if none is found.
func (h *Handler) contactContext(c *gin.Context, userID, orgID uuid.UUID, email string) string {
	if email == "" || h.ContactService == nil {
		return ""
	}
	res, xerr := h.ContactService.Search(c.Request.Context(), orgID.String(), "", "", "5", models.SearchContacts{Query: email})
	if xerr != nil || res == nil || len(res.Data) == 0 {
		return ""
	}
	detail, dxerr := h.ContactService.GetDetail(c.Request.Context(), userID, &orgID, res.Data[0].ID)
	if dxerr != nil || detail == nil {
		return ""
	}
	var b strings.Builder
	name := strings.TrimSpace(detail.FirstName + " " + detail.LastName)
	if name != "" {
		fmt.Fprintf(&b, "Contact: %s", name)
		if detail.Company != "" {
			fmt.Fprintf(&b, " at %s", detail.Company)
		}
		b.WriteString("\n")
	}
	if len(detail.CustomFields) > 0 {
		parts := make([]string, 0, len(detail.CustomFields))
		for k, v := range detail.CustomFields {
			parts = append(parts, k+": "+v)
		}
		fmt.Fprintf(&b, "Known details: %s\n", strings.Join(parts, ", "))
	}
	if len(detail.Campaigns) > 0 {
		names := make([]string, 0, len(detail.Campaigns))
		for _, cp := range detail.Campaigns {
			names = append(names, cp.Name)
		}
		fmt.Fprintf(&b, "In your campaigns: %s\n", strings.Join(names, ", "))
	}
	return strings.TrimSpace(b.String())
}

func buildReplyPrompt(history, contactCtx, instruction string) string {
	var b strings.Builder
	b.WriteString("Thread so far (oldest first):\n\n")
	b.WriteString(history)
	if contactCtx != "" {
		b.WriteString("\n\n")
		b.WriteString(contactCtx)
	}
	b.WriteString("\n\nWrite a reply to the most recent message in this thread.")
	if strings.TrimSpace(instruction) != "" {
		fmt.Fprintf(&b, " The user wants: %s", strings.TrimSpace(instruction))
	}
	return b.String()
}

// mapCreditError writes the standard 402/429 for a credit consume error.
func mapCreditError(c *gin.Context, err error) {
	switch {
	case isInsufficientCredits(err):
		paymentRequiredJSON(c, "You're out of AI credits. Add more to keep using AI features.")
	case isCapExceeded(err):
		errx.JSON(c, errx.New(errx.TooManyRequests, "AI usage limit reached, please try again later."))
	default:
		errx.JSON(c, errx.InternalError())
	}
}
