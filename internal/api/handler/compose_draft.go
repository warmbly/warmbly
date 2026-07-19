// AI draft for the compose window. Unlike the generic writing assistant this
// is grounded the same way reply drafts are: the recipient's contact record
// (custom fields, campaign membership), the full correspondence history with
// that address, and the org voice profile all feed the prompt. And instead of
// inventing a purpose when it has none, the model is told to ask: a response
// can be a single clarifying question the composer surfaces to the user.
package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// composeQuestionPrefix marks a model response that is a clarifying question
// rather than a draft. Kept out of user-visible text by the handler.
const composeQuestionPrefix = "QUESTION:"

const composeQuestionRule = `

MISSING PURPOSE: if you genuinely cannot tell WHAT this email should accomplish (no instruction, no subject, and the history gives no hint), do not invent a pitch. Respond with a single line starting with "QUESTION: " followed by one short, specific question that would let you write the email (for example what the user wants to offer, ask, or follow up on). Ask only when essential; if the context is enough, write the email.`

// DraftCompose — POST /unibox/compose/draft
func (h *Handler) DraftCompose(c *gin.Context) {
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
	if allowed, xerr := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID); xerr != nil {
		errx.JSON(c, xerr)
		return
	} else if !allowed {
		errx.JSON(c, errx.New(errx.Forbidden, "The unified inbox requires an active trial or paid subscription."))
		return
	}

	var req struct {
		// To is the recipient address; empty is allowed (no grounding yet).
		To          string `json:"to"`
		Subject     string `json:"subject"`
		Instruction string `json:"instruction"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	address := bareAddress(req.To)

	// Grounding: contact record + prior correspondence with the address.
	contactCtx := ""
	historyBlock := ""
	historyCount := 0
	if address != "" {
		contactCtx = h.contactContext(c, userID, *orgID, address)
		historyBlock, historyCount = h.composeHistoryContext(c, address)
	}

	paid, _ := h.FeatureGateService.IsPaidOrganization(c.Request.Context(), *orgID)
	model := h.AIProvider.ModelForTier(paid)
	voice := h.orgVoice(c.Request.Context(), *orgID, "")
	hasVoice := strings.TrimSpace(voice.ProductDescription) != "" ||
		strings.TrimSpace(voice.ICPNotes) != "" ||
		strings.TrimSpace(voice.VoiceProfile) != ""

	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	local := h.AIProvider.IsLocal()
	reqCtx := c.Request.Context()
	{
		meta := models.CreditMeta{Context: models.CreditContext{Detail: "compose draft to " + address}}
		if actor, aerr := middleware.GetUserUUID(c); aerr == nil {
			meta.ActorID = actor
		}
		reqCtx = models.WithCreditMeta(reqCtx, meta)
	}

	var remaining int
	if local {
		if bal, berr := h.CreditService.GetBalance(reqCtx, *orgID); berr == nil {
			remaining = bal
		}
	} else {
		var cerr error
		remaining, cerr = h.CreditService.Consume(reqCtx, *orgID, credits.CostReplyDraft, "compose_draft", model, 0, idemKey)
		if cerr != nil {
			mapCreditError(c, cerr)
			return
		}
	}

	system := generation.BuildVoiceRules(voice) + composeQuestionRule
	if h.SkillsService != nil {
		if pre := h.SkillsService.EnabledPreamble(c.Request.Context(), *orgID); pre != "" {
			system += "\n\n" + pre
		}
	}

	prompt := buildComposePrompt(address, contactCtx, historyBlock, req.Subject, req.Instruction)
	result, gerr := h.AIProvider.Complete(c.Request.Context(), generation.CompletionRequest{
		System: system,
		Prompt: prompt,
		Model:  model,
	})
	if gerr != nil {
		if !local {
			if bal, rerr := h.CreditService.Grant(reqCtx, *orgID, credits.CostReplyDraft, "compose_draft_refund"); rerr == nil {
				remaining = bal
			}
		}
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "The email drafter is temporarily unavailable. Your credits were not charged."))
		return
	}

	charged := 0
	if !local {
		charged = credits.CostReplyDraft
		if extra, serr := h.CreditService.SettleUsage(reqCtx, *orgID, credits.CostReplyDraft, result.Model, result.TokensUsed, "compose_draft", settleKey(idemKey)); serr == nil && extra > 0 {
			remaining -= extra
			charged += extra
		}
	}

	resp := gin.H{
		"credits_remaining": remaining,
		"credits_charged":   charged,
		"tokens_used":       result.TokensUsed,
		"model":             result.Model,
		"grounding": gin.H{
			"contact":       contactCtx != "",
			"history":       historyCount,
			"voice_profile": hasVoice,
		},
	}

	text := strings.TrimSpace(result.Text)
	if q, ok := strings.CutPrefix(text, composeQuestionPrefix); ok {
		resp["question"] = strings.TrimSpace(strings.SplitN(q, "\n", 2)[0])
	} else {
		resp["text"] = text
	}
	c.JSON(http.StatusOK, resp)
}

// composeHistoryContext renders the most recent conversations with the address
// (both directions) as a compact transcript block, newest first.
func (h *Handler) composeHistoryContext(c *gin.Context, address string) (string, int) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		return "", 0
	}
	oid := middleware.GetOrganizationID(c)
	if oid == nil {
		return "", 0
	}
	res, xerr := h.UniboxService.Search(c.Request.Context(), *oid, uid, &models.MailSearchParams{
		Address:  &address,
		PageSize: 8,
	})
	if xerr != nil || res == nil || len(res.Data) == 0 {
		return "", 0
	}
	var b strings.Builder
	for _, m := range res.Data {
		from := strings.Join(m.FromAddr, ", ")
		fmt.Fprintf(&b, "From: %s\nSubject: %s\n%s\n\n", from, m.Subject, strings.TrimSpace(m.Snippet))
	}
	return strings.TrimSpace(b.String()), len(res.Data)
}

func buildComposePrompt(address, contactCtx, history, subject, instruction string) string {
	var b strings.Builder
	b.WriteString("You are writing a brand-new outbound email (not a reply).\n")
	if address != "" {
		fmt.Fprintf(&b, "\nRecipient: %s\n", address)
	} else {
		b.WriteString("\nRecipient: not chosen yet.\n")
	}
	if contactCtx != "" {
		b.WriteString(contactCtx)
		b.WriteString("\n")
	} else if address != "" {
		b.WriteString("No contact record for this address.\n")
	}
	if history != "" {
		b.WriteString("\nPrevious correspondence with them (newest first):\n\n")
		b.WriteString(history)
		b.WriteString("\n")
	} else if address != "" {
		b.WriteString("\nNo previous correspondence with this address; this is a first touch.\n")
	}
	if s := strings.TrimSpace(subject); s != "" {
		fmt.Fprintf(&b, "\nSubject line the user already wrote: %s\n", s)
	}
	b.WriteString("\nWrite the complete email body, ready to send. Return ONLY the body text: no subject line, no signature, no placeholders like [Name]; if a detail is unknown, write around it.")
	if inst := strings.TrimSpace(instruction); inst != "" {
		fmt.Fprintf(&b, "\nThe user wants: %s", inst)
	}
	return b.String()
}
