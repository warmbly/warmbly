package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// writingCall is one writing-assistant generation the balance was checked for;
// deliverWriting runs it, charges what it used and answers.
type writingCall struct {
	orgID   uuid.UUID
	reqCtx  context.Context // carries the credit attribution
	local   bool
	ledger  string // usage reason
	idemKey string
	model   string
	voice   generation.VoiceContext
	// source is text the author supplied; flagged words already in it are theirs.
	source string
	// tidy cleans the model's raw text before the final version is sent.
	tidy func(string) string
	// generate runs the model. onDelta is nil unless the caller is streaming.
	generate func(ctx context.Context, onDelta func(string)) (*generation.WritingResult, error)
}

// writingEvent is one Server-Sent Event of a streamed generation: "delta" (a
// fragment as the model writes it), "stage" ("checking" while the spam-filter
// pass runs), then exactly one "done" (the final text and what it cost) or
// "error".
type writingEvent struct {
	Type             string   `json:"type"`
	Text             string   `json:"text,omitempty"`
	Stage            string   `json:"stage,omitempty"`
	CreditsRemaining *int     `json:"credits_remaining,omitempty"`
	CreditsCharged   *int     `json:"credits_charged,omitempty"`
	ChargedUSD       *float64 `json:"charged_usd,omitempty"`
	BalanceUSD       *float64 `json:"balance_usd,omitempty"`
	TokensUsed       *int     `json:"tokens_used,omitempty"`
	Model            string   `json:"model,omitempty"`
	Code             string   `json:"code,omitempty"`
	Message          string   `json:"message,omitempty"`
}

// wantsEventStream reports whether the caller asked for the streamed answer.
func wantsEventStream(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "text/event-stream")
}

// deliverWriting answers with one JSON body or, when the caller accepts
// text/event-stream, streams the text while the model writes it and closes
// with the final version. The two can differ: fences are tidied and flagged
// words reworded once the model has finished.
func (h *Handler) deliverWriting(c *gin.Context, w writingCall) {
	ctx := c.Request.Context()
	var emit func(writingEvent)
	if wantsEventStream(c) {
		emit = sseEmitter[writingEvent](c)
	}
	var onDelta func(string)
	if emit != nil {
		onDelta = func(s string) { emit(writingEvent{Type: "delta", Text: s}) }
	}

	result, gerr := w.generate(ctx, onDelta)
	if gerr != nil {
		// A failed generation reports no usage, so nothing is charged.
		msg := "The writing assistant is temporarily unavailable. Nothing was charged."
		if errors.Is(gerr, generation.ErrNotConfigured) {
			msg = "AI writing assistant is not configured."
		}
		if emit != nil {
			emit(writingEvent{Type: "error", Code: "service_unavailable", Message: msg})
			return
		}
		errx.JSON(c, errx.New(errx.ServiceUnavailable, msg))
		return
	}

	text := result.Text
	if w.tidy != nil {
		text = w.tidy(text)
	}
	onPass := func() {
		if emit != nil {
			emit(writingEvent{Type: "stage", Stage: "checking"})
		}
	}
	if fixed, in, out := h.rewordFlagged(ctx, w.model, w.voice, text, w.source, onPass); in+out > 0 {
		text = fixed
		result.TokensUsed += in + out
		result.InputTokens += in
		result.OutputTokens += out
	}

	usage := credits.AIUsage(result.Model, result.InputTokens, result.OutputTokens)
	charged, balance := h.chargeAI(w.reqCtx, w.orgID, w.local, usage, w.ledger, w.idemKey)

	if emit != nil {
		remaining, chargedCredits := models.CreditsFromMicro(balance), models.CreditsFromMicro(charged)
		chargedUSD, balanceUSD := charged.USD(), balance.USD()
		emit(writingEvent{
			Type: "done", Text: text, Model: result.Model,
			CreditsRemaining: &remaining, CreditsCharged: &chargedCredits,
			ChargedUSD: &chargedUSD, BalanceUSD: &balanceUSD, TokensUsed: &result.TokensUsed,
		})
		return
	}
	c.JSON(http.StatusOK, withBalance(gin.H{
		"text":        text,
		"tokens_used": result.TokensUsed,
		"model":       result.Model,
	}, charged, balance))
}
