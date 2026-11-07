// AI writing-assistant generation endpoint. Flow:
//  1. feature-gate the org (paid or in free trial) via CanUseWritingAssistant
//  2. check the balance can pay for a typical call (nothing is taken)
//  3. call the configured provider (Anthropic, falling back to OpenAI)
//  4. charge the tokens it actually used, once per Idempotency-Key
//  5. return {text, balance, model}, or stream the text as it is written when
//     the caller accepts text/event-stream (deliverWriting)
//
// A short balance answers 402 BEFORE any provider call, so a depleted org
// never burns a paid completion, and a failed call is never charged.

package handler

import (
	"context"
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

// writeMaxPromptLen bounds the inbound prompt so a single request can't be used
// to drive a very large (and expensive) completion.
const writeMaxPromptLen = 8000

type generationWriteRequest struct {
	Prompt string `json:"prompt"`
	Tone   string `json:"tone"`
}

// paymentRequiredJSON emits the standard error envelope with a 402 status.
// errx has no PaymentRequired code, so this endpoint writes the 402 directly
// while keeping the same {error, message, code, request_id} shape.
func paymentRequiredJSON(c *gin.Context, message string) {
	c.JSON(http.StatusPaymentRequired, gin.H{
		"error":      "Payment Required",
		"message":    message,
		"code":       "insufficient_credits",
		"request_id": c.GetString("request_id"),
	})
}

// GenerateWriting — POST /generation/write
func (h *Handler) GenerateWriting(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req generationWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "prompt is required"))
		return
	}
	if len(req.Prompt) > writeMaxPromptLen {
		errx.JSON(c, errx.New(errx.BadRequest, "prompt is too long"))
		return
	}

	// Feature gate: paid orgs and free-trial orgs may use the assistant.
	allowed, xerr := h.FeatureGateService.CanUseWritingAssistant(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if !allowed {
		errx.JSON(c, errx.New(errx.Forbidden, "The AI writing assistant requires an active plan or trial."))
		return
	}

	// Provider must be configured.
	if h.WritingGenerator == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI writing assistant is not configured."))
		return
	}

	// Model routing by tier. Paid orgs get the stronger model; the active
	// provider (Anthropic or OpenAI fallback) decides the concrete model ID.
	paid, xerr := h.FeatureGateService.IsPaidOrganization(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	model := h.WritingGenerator.ModelForTier(paid)

	// A free/local model (AI_FREE) runs un-metered.
	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	local := h.WritingGenerator.IsLocal()
	// Attribute the charge to the teammate who asked for the draft.
	reqCtx := c.Request.Context()
	if actor, aerr := middleware.GetUserUUID(c); aerr == nil {
		reqCtx = models.WithCreditMeta(reqCtx, models.CreditMeta{ActorID: actor})
	}
	if !h.reserveAI(c, reqCtx, *orgID, local, credits.EstimateWritingAssistant) {
		return
	}

	voice := h.orgVoice(c.Request.Context(), *orgID, req.Tone)
	h.deliverWriting(c, writingCall{
		orgID: *orgID, reqCtx: reqCtx, local: local,
		ledger: "writing_assistant", idemKey: idemKey,
		model: model, voice: voice,
		generate: func(ctx context.Context, onDelta func(string)) (*generation.WritingResult, error) {
			if s, ok := h.WritingGenerator.(generation.StreamCompleter); ok && onDelta != nil {
				return s.CompleteStream(ctx, generation.WritingRequest(model, req.Prompt, voice), onDelta)
			}
			return h.WritingGenerator.GenerateWriting(ctx, model, req.Prompt, voice)
		},
	})
}

// reserveAI checks, before a metered AI call, that the workspace can pay for
// it, answering a refusal itself; false after answering. A free/local model is
// never metered.
func (h *Handler) reserveAI(c *gin.Context, ctx context.Context, orgID uuid.UUID, local bool, estimate models.Micro) bool {
	if local {
		return true
	}
	if err := h.CreditService.Reserve(ctx, orgID, estimate); err != nil {
		mapCreditError(c, err)
		return false
	}
	return true
}

// chargeAI takes what a delivered AI call used, once per key (nothing on a
// free/local model), and reads the balance after it for the answer.
func (h *Handler) chargeAI(ctx context.Context, orgID uuid.UUID, local bool, u credits.Usage, reason, idemKey string) (charged, balance models.Micro) {
	if !local {
		// Best-effort: a delivered result never fails on its charge.
		charged, _ = h.CreditService.Charge(ctx, orgID, u, reason, idemKey)
	}
	return charged, h.balanceNow(ctx, orgID)
}

// balanceNow is the workspace balance for an answer; zero when unreadable.
func (h *Handler) balanceNow(ctx context.Context, orgID uuid.UUID) models.Micro {
	bal, xerr := h.CreditService.GetBalance(ctx, orgID)
	if xerr != nil {
		return 0
	}
	return bal
}

// withBalance adds what a call cost and the balance after it to an answer, in
// dollars beside the legacy credit figures older clients read.
func withBalance(out gin.H, charged, balance models.Micro) gin.H {
	out["credits_charged"] = models.CreditsFromMicro(charged)
	out["credits_remaining"] = models.CreditsFromMicro(balance)
	out["charged_usd"] = charged.USD()
	out["balance_usd"] = balance.USD()
	return out
}
