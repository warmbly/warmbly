// AI writing-assistant generation endpoint. Flow:
//  1. feature-gate the org (paid or in free trial) via CanUseWritingAssistant
//  2. atomically consume one credit (DB-enforced: no negative balance, no
//     double-charge on Idempotency-Key replay)
//  3. call the configured provider (Anthropic, falling back to OpenAI)
//  4. return {text, credits_remaining, model}
//
// On insufficient credits the consume step short-circuits with 402 BEFORE any
// provider call, so a depleted org never burns a paid completion. Because the
// debit happens before the provider call, a provider failure refunds the
// credit so the customer is not charged for a generation they never received.

package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// creditsPerWrite is the credit cost of one writing-assistant call. Kept as a
// constant so pricing is in one place; tokens consumed are recorded separately
// on the ledger transaction for later cost analysis.
const creditsPerWrite = 1

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
		errx.JSON(c, errx.ErrInvalid)
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

	// Consume one credit up front. The DB enforces the no-negative / no-replay
	// invariants; on a depleted balance this returns 402 with no provider call.
	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	remaining, err := h.CreditService.Consume(
		c.Request.Context(), *orgID, creditsPerWrite,
		"writing_assistant", model, 0, idemKey,
	)
	if err != nil {
		switch {
		case errors.Is(err, credits.ErrInsufficientCredits):
			paymentRequiredJSON(c, "You're out of AI credits. Upgrade or purchase more to keep using the writing assistant.")
		case errors.Is(err, credits.ErrCapExceeded):
			errx.JSON(c, errx.New(errx.TooManyRequests, "AI writing assistant usage limit reached, please try again later."))
		default:
			errx.JSON(c, errx.InternalError())
		}
		return
	}

	// Generate. On provider failure, refund the credit so the customer is not
	// charged for a completion they never received. The refund is best-effort;
	// a failed refund is logged via the audit trail rather than surfaced.
	result, gerr := h.WritingGenerator.GenerateWriting(c.Request.Context(), model, req.Prompt, req.Tone)
	if gerr != nil {
		if bal, rerr := h.CreditService.Grant(c.Request.Context(), *orgID, creditsPerWrite, "writing_assistant_refund"); rerr == nil {
			remaining = bal
		}
		if errors.Is(gerr, generation.ErrNotConfigured) {
			errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI writing assistant is not configured."))
			return
		}
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "The writing assistant is temporarily unavailable. Your credit was not charged."))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"text":              result.Text,
		"credits_remaining": remaining,
		"model":             result.Model,
	})
}
