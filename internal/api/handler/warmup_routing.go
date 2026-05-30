package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// warmupRoutingRulePayload is the wire shape for create/update requests.
// Kept separate from models.WarmupRoutingRule so server-managed fields
// (id, organization_id, created_at, updated_at) cannot be set by clients.
type warmupRoutingRulePayload struct {
	Name                string  `json:"name"`
	Priority            int     `json:"priority"`
	SenderMatchType     string  `json:"sender_match_type"`
	SenderMatchValue    string  `json:"sender_match_value"`
	RecipientMatchType  string  `json:"recipient_match_type"`
	RecipientMatchValue string  `json:"recipient_match_value"`
	Weight              float64 `json:"weight"`
	Enabled             bool    `json:"enabled"`
}

func (p *warmupRoutingRulePayload) toModel(orgID, ruleID uuid.UUID) *models.WarmupRoutingRule {
	return &models.WarmupRoutingRule{
		ID:                  ruleID,
		OrganizationID:      orgID,
		Name:                strings.TrimSpace(p.Name),
		Priority:            p.Priority,
		SenderMatchType:     models.WarmupRoutingMatchType(p.SenderMatchType),
		SenderMatchValue:    strings.ToLower(strings.TrimSpace(p.SenderMatchValue)),
		RecipientMatchType:  models.WarmupRoutingMatchType(p.RecipientMatchType),
		RecipientMatchValue: strings.ToLower(strings.TrimSpace(p.RecipientMatchValue)),
		Weight:              p.Weight,
		Enabled:             p.Enabled,
	}
}

func validateRoutingPayload(p *warmupRoutingRulePayload) *errx.Error {
	if strings.TrimSpace(p.Name) == "" {
		return errx.New(errx.BadRequest, "name is required")
	}
	if !isValidMatchType(p.SenderMatchType) {
		return errx.New(errx.BadRequest, "invalid sender_match_type")
	}
	if !isValidMatchType(p.RecipientMatchType) {
		return errx.New(errx.BadRequest, "invalid recipient_match_type")
	}
	if p.SenderMatchType != string(models.WarmupMatchAny) && p.SenderMatchValue == "" {
		return errx.New(errx.BadRequest, "sender_match_value is required for non-any match types")
	}
	if p.RecipientMatchType != string(models.WarmupMatchAny) && p.RecipientMatchValue == "" {
		return errx.New(errx.BadRequest, "recipient_match_value is required for non-any match types")
	}
	if p.Weight < 0 {
		return errx.New(errx.BadRequest, "weight must be >= 0")
	}
	return nil
}

func isValidMatchType(t string) bool {
	switch models.WarmupRoutingMatchType(t) {
	case models.WarmupMatchAny, models.WarmupMatchDomain, models.WarmupMatchTLD, models.WarmupMatchProvider:
		return true
	}
	return false
}

// ListWarmupRoutingRules returns every rule for the caller's organization,
// ordered by priority (ascending — first to evaluate).
func (h *Handler) ListWarmupRoutingRules(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	rules, err := h.WarmupRoutingRepo.ListForOrganization(c.Request.Context(), orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rules"})
		return
	}
	if rules == nil {
		rules = []models.WarmupRoutingRule{}
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

// CreateWarmupRoutingRule creates a new rule for the caller's organization.
func (h *Handler) CreateWarmupRoutingRule(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var payload warmupRoutingRulePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if xerr := validateRoutingPayload(&payload); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	rule := payload.toModel(orgID, uuid.New())
	if err := h.WarmupRoutingRepo.Create(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rule"})
		return
	}
	c.JSON(http.StatusCreated, rule)
}

// UpdateWarmupRoutingRule updates a rule by ID.
func (h *Handler) UpdateWarmupRoutingRule(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	ruleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}
	var payload warmupRoutingRulePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if xerr := validateRoutingPayload(&payload); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	rule := payload.toModel(orgID, ruleID)
	if err := h.WarmupRoutingRepo.Update(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rule)
}

// DeleteWarmupRoutingRule removes a rule by ID.
func (h *Handler) DeleteWarmupRoutingRule(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	ruleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}
	if err := h.WarmupRoutingRepo.Delete(c.Request.Context(), orgID, ruleID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// requireOrgID resolves the caller's organization ID via the auth middleware
// helper, writing a 403 and returning ok=false when no org is set.
func requireOrgID(c *gin.Context) (uuid.UUID, bool) {
	if orgID := middleware.GetOrganizationID(c); orgID != nil {
		return *orgID, true
	}
	errx.JSON(c, errx.New(errx.Forbidden, "organization context required"))
	return uuid.Nil, false
}
