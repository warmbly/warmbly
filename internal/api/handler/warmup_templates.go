package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
	"github.com/warmbly/warmbly/internal/pkg/warmupcheck"
	"github.com/warmbly/warmbly/internal/repository"
)

// Bounds for an organization-supplied warmup template. Templates go into the
// shared content bank, so they share the opening's shape: a short subject and
// a plain-text body.
const (
	warmupTemplateMaxSubject = 200
	warmupTemplateMaxBody    = 50 * 1024
	warmupTemplatePoolType   = "premium"
)

// warmupTemplatePayload is the wire shape for create and check requests.
// Server-managed fields (id, organization_id, counters) cannot be set.
type warmupTemplatePayload struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Segment string `json:"segment"`
}

// warmupTemplate is the slim wire shape for list and create responses.
type warmupTemplate struct {
	ID         uuid.UUID                       `json:"id"`
	Name       string                          `json:"name"`
	Subject    string                          `json:"subject"`
	Body       string                          `json:"body,omitempty"`
	Segment    string                          `json:"segment"`
	UsageCount int64                           `json:"usage_count"`
	CreatedAt  time.Time                       `json:"created_at"`
	Placement  *models.WarmupTemplatePlacement `json:"placement,omitempty"`
}

// warmupTemplateStatsDefaultDays is the trailing window for template placement.
const warmupTemplateStatsDefaultDays = 30

// templateStatsSince reads the optional days query (1 to 90) into a cutoff.
func templateStatsSince(c *gin.Context) (time.Time, int, bool) {
	days := warmupTemplateStatsDefaultDays
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 90 {
			errx.JSON(c, errx.New(errx.BadRequest, "days must be between 1 and 90"))
			return time.Time{}, 0, false
		}
		days = n
	}
	return time.Now().UTC().AddDate(0, 0, -days), days, true
}

func templateResponse(c models.WarmupConversation, withBody bool) warmupTemplate {
	t := warmupTemplate{
		ID:         c.ID,
		Name:       c.Theme,
		Subject:    c.Subject,
		Segment:    c.Segment,
		UsageCount: c.UsageCount,
		CreatedAt:  c.CreatedAt,
	}
	if withBody {
		t.Body = c.Description
	}
	return t
}

// validateTemplatePayload enforces the same guardrails the send path keeps,
// so a stored template can never fail its own send-time check.
func validateTemplatePayload(p *warmupTemplatePayload) *errx.Error {
	p.Name = strings.TrimSpace(p.Name)
	p.Subject = strings.TrimSpace(p.Subject)
	p.Body = strings.TrimSpace(p.Body)
	p.Segment = strings.TrimSpace(p.Segment)
	if p.Subject == "" {
		return errx.New(errx.BadRequest, "subject is required")
	}
	if utf8.RuneCountInString(p.Subject) > warmupTemplateMaxSubject {
		return errx.New(errx.BadRequest, "subject exceeds 200 characters")
	}
	if strings.ContainsAny(p.Subject, "<>{}\x00\r\n") {
		return errx.New(errx.BadRequest, "subject contains invalid characters")
	}
	if p.Name != "" && utf8.RuneCountInString(p.Name) > warmupTemplateMaxSubject {
		return errx.New(errx.BadRequest, "name exceeds 200 characters")
	}
	if p.Body == "" {
		return errx.New(errx.BadRequest, "body is required")
	}
	if len(p.Body) > warmupTemplateMaxBody {
		return errx.New(errx.BadRequest, "body exceeds 50 KB")
	}
	if strings.IndexByte(p.Body, 0) >= 0 {
		return errx.New(errx.BadRequest, "body contains invalid characters")
	}
	if utf8.RuneCountInString(p.Segment) > warmupTemplateMaxSubject {
		return errx.New(errx.BadRequest, "segment exceeds 200 characters")
	}
	if err := generation.ValidateWarmupWording(p.Subject, p.Body); err != nil {
		return errx.New(errx.BadRequest, err.Error())
	}
	// The send path runs the same hard gate; rejecting here means a stored template always sends.
	if err := warmlint.Check(p.Subject, p.Body, false); err != nil {
		return errx.New(errx.BadRequest, "template would be blocked at send time: "+err.Error())
	}
	return nil
}

// CreateWarmupTemplate stores one organization's subject + opening body as a
// verbatim warmup conversation; there is no cap on how many a workspace
// keeps, and the pick path interleaves each into that organization's own
// sends at most once per sender per day.
func (h *Handler) CreateWarmupTemplate(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var payload warmupTemplatePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if xerr := validateTemplatePayload(&payload); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	scenario, rendering := generation.WarmupScenarioVersion, generation.CanonicalRenderingVersion
	conv := models.WarmupConversation{
		PoolType:         warmupTemplatePoolType,
		Segment:          payload.Segment,
		Source:           models.WarmupContentSourceTemplate,
		Theme:            payload.Name,
		Subject:          payload.Subject,
		Description:      payload.Body,
		Messages:         []string{},
		Status:           models.WarmupConversationActive,
		LintPassed:       true,
		ReplyEligible:    true,
		ScenarioVersion:  &scenario,
		RenderingVersion: &rendering,
		SemanticReview:   "passed",
		OrganizationID:   &orgID,
	}
	if err := h.WarmupContentRepo.InsertConversation(c.Request.Context(), &conv); err != nil {
		if errors.Is(err, repository.ErrWarmupConversationDuplicate) {
			errx.JSON(c, errx.New(errx.Conflict, "subject or body already exists in your warmup templates"))
			return
		}
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}

	if stored, err := h.WarmupContentRepo.GetConversation(c.Request.Context(), conv.ID); err == nil && stored != nil {
		conv = *stored
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityWarmupTemplate, &conv.ID, nil, map[string]string{
		"name": conv.Theme,
	})

	c.JSON(http.StatusCreated, gin.H{
		"template": templateResponse(conv, true),
		"check":    warmupcheck.Check(conv.Subject, conv.Description),
	})
}

// ListWarmupTemplates returns the caller's organization's active templates
// with each one's placement over the trailing window (days, default 30).
func (h *Handler) ListWarmupTemplates(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	since, days, ok := templateStatsSince(c)
	if !ok {
		return
	}
	convs, err := h.listAllWarmupTemplates(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	placement, err := h.WarmupContentRepo.TemplatePlacement(c.Request.Context(), orgID, since)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	templates := make([]warmupTemplate, 0, len(convs))
	for _, conv := range convs {
		t := templateResponse(conv, false)
		p := placement[conv.ID]
		t.Placement = &p
		templates = append(templates, t)
	}
	c.JSON(http.StatusOK, gin.H{"templates": templates, "days": days})
}

// warmupTemplatePage is the repository page size; the list walks every page
// so a workspace with more templates than one page still sees them all.
const warmupTemplatePage = 200

func (h *Handler) listAllWarmupTemplates(ctx context.Context, orgID uuid.UUID) ([]models.WarmupConversation, error) {
	var all []models.WarmupConversation
	for offset := 0; ; offset += warmupTemplatePage {
		page, total, err := h.WarmupContentRepo.ListConversations(ctx, repository.ConversationFilter{
			Source:         models.WarmupContentSourceTemplate,
			Status:         models.WarmupConversationActive,
			OrganizationID: &orgID,
			Limit:          warmupTemplatePage,
			Offset:         offset,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < warmupTemplatePage || len(all) >= total {
			return all, nil
		}
	}
}

// GetWarmupTemplateStats is one template's placement split by recipient
// provider group over the trailing window (days, default 30).
func (h *Handler) GetWarmupTemplateStats(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid template id"))
		return
	}
	since, days, ok := templateStatsSince(c)
	if !ok {
		return
	}
	conv, err := h.WarmupContentRepo.GetOrgTemplateConversation(c.Request.Context(), templateID, orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if conv == nil {
		errx.JSON(c, errx.New(errx.NotFound, "warmup template not found"))
		return
	}
	groups, err := h.WarmupContentRepo.TemplatePlacementByGroup(c.Request.Context(), templateID, orgID, since)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	var total models.WarmupTemplatePlacement
	for _, g := range groups {
		total.Sent += g.Sent
		total.Inbox += g.Inbox
		total.Tabs += g.Tabs
		total.Spam += g.Spam
		total.Rescued += g.Rescued
	}
	c.JSON(http.StatusOK, gin.H{"template_id": templateID, "days": days, "total": total, "groups": groups})
}

// GetWarmupTemplateSettings returns how much of the organization's warmup
// carries its own templates.
func (h *Handler) GetWarmupTemplateSettings(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	settings, err := h.WarmupContentRepo.GetTemplateSettings(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, settings)
}

// UpdateWarmupTemplateSettings sets the template share (0 to 100) and the
// templates-only switch for the organization.
func (h *Handler) UpdateWarmupTemplateSettings(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var payload struct {
		TemplateShare *int  `json:"template_share"`
		TemplatesOnly *bool `json:"templates_only"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if payload.TemplateShare == nil || payload.TemplatesOnly == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "template_share and templates_only are required"))
		return
	}
	settings := models.WarmupTemplateSettings{TemplateShare: *payload.TemplateShare, TemplatesOnly: *payload.TemplatesOnly}
	if settings.TemplateShare < 0 || settings.TemplateShare > 100 {
		errx.JSON(c, errx.New(errx.BadRequest, "template_share must be between 0 and 100"))
		return
	}
	if err := h.WarmupContentRepo.UpsertTemplateSettings(c.Request.Context(), orgID, settings); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityWarmupTemplate, nil, nil, map[string]string{
		"template_share": strconv.Itoa(settings.TemplateShare),
		"templates_only": strconv.FormatBool(settings.TemplatesOnly),
	})
	c.JSON(http.StatusOK, settings)
}

// DeleteWarmupTemplate archives one of the caller's organization's templates.
func (h *Handler) DeleteWarmupTemplate(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid template id"))
		return
	}
	archived, err := h.WarmupContentRepo.ArchiveOrgTemplateConversation(c.Request.Context(), templateID, orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if !archived {
		errx.JSON(c, errx.New(errx.NotFound, "warmup template not found"))
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityWarmupTemplate, &templateID, nil, nil)

	c.Status(http.StatusNoContent)
}

// CheckWarmupTemplate runs the static analyzer over a draft subject + body
// without storing anything, so a workspace can inspect copy before submit.
func (h *Handler) CheckWarmupTemplate(c *gin.Context) {
	if _, ok := requireOrgID(c); !ok {
		return
	}
	var payload warmupTemplatePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if utf8.RuneCountInString(payload.Subject) > warmupTemplateMaxSubject || len(payload.Body) > warmupTemplateMaxBody {
		errx.JSON(c, errx.New(errx.BadRequest, "subject exceeds 200 characters or body exceeds 50 KB"))
		return
	}
	c.JSON(http.StatusOK, warmupcheck.Check(payload.Subject, payload.Body))
}
