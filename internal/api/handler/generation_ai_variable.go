// AI-variable preview endpoint: generate the recipient-specific snippet a
// per-recipient AI block would produce, so the campaign editor's "Preview"
// button shows real output. Same credit flow as /generation/write and
// /generation/edit (gate, consume up front, refund on provider failure, usage
// settle), and the exact snippet framing the send path uses (tasks.BuildAIVariablePrompt)
// so preview and send never drift. The prompt renders against a supplied contact
// (org-scoped; 404 if not in the org) or a sample contact.

package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/tasks"
)

// aiVarPreviewMaxPromptLen bounds the block prompt; aiVarPreviewMaxTokens caps
// the completion (matches the send-path resolver).
const (
	aiVarPreviewMaxPromptLen = 8000
	aiVarPreviewMaxTokens    = 400
	aiVarPreviewTimeout      = 20 * time.Second
)

type generationAIVariableRequest struct {
	Mode      string `json:"mode"` // "instant" | "research"
	Prompt    string `json:"prompt"`
	Tone      string `json:"tone"`
	WebSearch bool   `json:"web_search"`
	ContactID string `json:"contact_id"`
	// The email text on either side of the block, so the fragment fits the
	// sentence it lands in (matches the send path). Optional.
	ContextBefore string `json:"context_before"`
	ContextAfter  string `json:"context_after"`
}

// GenerateAIVariable — POST /generation/ai-variable
func (h *Handler) GenerateAIVariable(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req generationAIVariableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "prompt is required"))
		return
	}
	if len(req.Prompt) > aiVarPreviewMaxPromptLen {
		errx.JSON(c, errx.New(errx.BadRequest, "prompt is too long"))
		return
	}

	// Feature gate: paid orgs and free-trial orgs may use AI generation.
	allowed, xerr := h.FeatureGateService.CanUseWritingAssistant(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if !allowed {
		errx.JSON(c, errx.New(errx.Forbidden, "AI variables require an active plan or trial."))
		return
	}
	if h.AIProvider == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI generation is not configured."))
		return
	}

	// Resolve the contact to render the prompt against: a supplied id (must be in
	// the org) or a sample contact.
	contact := sampleContact()
	if strings.TrimSpace(req.ContactID) != "" {
		cid, perr := uuid.Parse(strings.TrimSpace(req.ContactID))
		if perr != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid contact_id"))
			return
		}
		if h.ContactRepo == nil {
			errx.JSON(c, errx.ErrNotFound)
			return
		}
		found, cxerr := h.ContactRepo.GetByIDsAndOrganization(c.Request.Context(), *orgID, []uuid.UUID{cid})
		if cxerr != nil {
			errx.JSON(c, cxerr)
			return
		}
		if len(found) == 0 {
			errx.JSON(c, errx.ErrNotFound)
			return
		}
		contact = found[0]
	}

	rendered := strings.TrimSpace(tasks.RenderTemplate(req.Prompt, contact))
	if rendered == "" {
		// A prompt that renders empty against the contact charges nothing.
		remaining := 0
		if bal, berr := h.CreditService.GetBalance(c.Request.Context(), *orgID); berr == nil {
			remaining = bal
		}
		c.JSON(http.StatusOK, gin.H{
			"text": "", "credits_remaining": remaining, "credits_charged": 0,
			"tokens_used": 0, "model": "",
		})
		return
	}

	// Cost mirrors the resolver: instant -> CostWritingAssistant, research ->
	// CostResearchRun. Research degrades to the same completion path here (no agent
	// tool loop on the preview) but still charges the research price.
	research := strings.EqualFold(strings.TrimSpace(req.Mode), "research")
	cost := credits.CostWritingAssistant
	reason := "campaign_ai_var_preview"
	if research {
		cost = credits.CostResearchRun
	}

	paid, xerr := h.FeatureGateService.IsPaidOrganization(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	model := h.AIProvider.ModelForTier(paid)

	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	local := h.AIProvider.IsLocal()
	reqCtx := c.Request.Context()
	if actor, aerr := middleware.GetUserUUID(c); aerr == nil {
		reqCtx = models.WithCreditMeta(reqCtx, models.CreditMeta{ActorID: actor})
	}

	var remaining int
	if local {
		if bal, berr := h.CreditService.GetBalance(reqCtx, *orgID); berr == nil {
			remaining = bal
		}
	} else {
		var err error
		remaining, err = h.CreditService.Consume(reqCtx, *orgID, cost, reason, model, 0, idemKey)
		if err != nil {
			switch {
			case errors.Is(err, credits.ErrInsufficientCredits):
				paymentRequiredJSON(c, "You're out of AI credits. Upgrade or purchase more to keep using AI variables.")
			case errors.Is(err, credits.ErrCapExceeded):
				errx.JSON(c, errx.New(errx.TooManyRequests, "AI usage limit reached, please try again later."))
			default:
				errx.JSON(c, errx.InternalError())
			}
			return
		}
	}

	// Optional web enrichment (one bounded lookup, charged only when it returns
	// results), matching the send-path resolver. Research implies web on.
	web := ""
	if (req.WebSearch || research) && h.AISearch != nil {
		if q := previewSearchQuery(contact); q != "" {
			sctx, scancel := context.WithTimeout(reqCtx, 15*time.Second)
			results, serr := h.AISearch.Search(sctx, q, 3)
			scancel()
			if serr == nil && len(results) > 0 {
				web = generation.FormatSearchResults(results)
				if !local {
					if _, cerr := h.CreditService.Consume(reqCtx, *orgID, credits.CostWebSearch, reason+"_search", "", 0, searchIdem(idemKey)); cerr == nil {
						remaining -= credits.CostWebSearch
					}
				}
			}
		}
	}

	// Ground the humanizer with the org's voice and the standard merge variables,
	// so preview matches the send path's shared voice rules. The standard five
	// tokens are enough here; per-contact custom keys are the send path's concern.
	vc := h.orgVoice(reqCtx, *orgID, req.Tone)
	vc.AvailableVars = generation.StandardMergeVars
	system, prompt := tasks.BuildAIVariablePrompt(vc, contact, rendered, web,
		clampPreviewContext(req.ContextBefore), clampPreviewContext(req.ContextAfter))

	cctx, cancel := context.WithTimeout(reqCtx, aiVarPreviewTimeout)
	defer cancel()
	result, gerr := h.AIProvider.Complete(cctx, generation.CompletionRequest{
		System:      system,
		Prompt:      prompt,
		Model:       model,
		MaxTokens:   aiVarPreviewMaxTokens,
		Temperature: generation.Deterministic(),
	})
	if gerr != nil || result == nil {
		if !local {
			if bal, rerr := h.CreditService.Grant(reqCtx, *orgID, cost, reason+"_refund"); rerr == nil {
				remaining = bal
			}
		}
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI generation is temporarily unavailable. Your credit was not charged."))
		return
	}

	charged := 0
	if !local {
		charged = cost
		if extra, serr := h.CreditService.SettleUsage(reqCtx, *orgID, cost, result.Model, result.TokensUsed, reason, settleKey(idemKey)); serr == nil && extra > 0 {
			remaining -= extra
			charged += extra
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"text":              strings.TrimSpace(result.Text),
		"credits_remaining": remaining,
		"credits_charged":   charged,
		"tokens_used":       result.TokensUsed,
		"model":             result.Model,
	})
}

// aiVarPreviewContextMax bounds each side of the surrounding-email context sent
// with a preview, so a caller cannot inflate the prompt.
const aiVarPreviewContextMax = 1200

func clampPreviewContext(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > aiVarPreviewContextMax {
		return string(r[:aiVarPreviewContextMax])
	}
	return s
}

// searchIdem derives the web-search settle/consume key from the call's key. An
// empty key stays empty (non-idempotent call, non-idempotent search charge).
func searchIdem(idemKey string) string {
	if idemKey == "" {
		return ""
	}
	return idemKey + ":search"
}

// previewSearchQuery derives a web-search query from the contact's own fields
// (company + name, then a corporate email domain), never from any external text,
// so a hostile field cannot steer the search. Free-mail domains are useless as a
// company signal and are skipped.
func previewSearchQuery(contact models.Contact) string {
	company := strings.TrimSpace(contact.Company)
	name := strings.TrimSpace(strings.TrimSpace(contact.FirstName) + " " + strings.TrimSpace(contact.LastName))
	if company != "" {
		return strings.TrimSpace(company + " " + name)
	}
	if at := strings.LastIndex(contact.Email, "@"); at >= 0 {
		domain := strings.ToLower(strings.TrimSpace(contact.Email[at+1:]))
		if domain != "" && !previewFreeMailDomains[domain] {
			return domain
		}
	}
	return ""
}

// previewFreeMailDomains never identify a company, so they are useless as a
// search fallback (mirrors the send-path resolver's list).
var previewFreeMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true,
	"live.com": true, "yahoo.com": true, "icloud.com": true, "me.com": true, "aol.com": true,
	"proton.me": true, "protonmail.com": true, "gmx.com": true, "mail.com": true,
}
