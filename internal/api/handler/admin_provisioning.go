package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/cloudprovider"
	"github.com/warmbly/warmbly/internal/infrastructure/cloudprovider/hetzner"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Admin endpoints under /admin for autonomous fleet management:
//
//   /admin/cloud-credentials                    CRUD + test
//   /admin/cloud-providers/:provider/...        discovery (locations, server_types, images)
//   /admin/provisioning-templates               CRUD
//   /admin/provisioning-jobs                    list + create + detail
//   /admin/provisioning-policy                  per-provider budget caps
//
// All gated by AdminPermManageSettings via the route registration.

// ---------------------------------------------------------------------------
// Cloud credentials
// ---------------------------------------------------------------------------

type CloudCredentialResponse struct {
	ID            uuid.UUID  `json:"id"`
	Provider      string     `json:"provider"`
	Name          string     `json:"name"`
	TokenMasked   string     `json:"token_masked"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	LastTestAt    *time.Time `json:"last_test_at,omitempty"`
	LastTestOK    *bool      `json:"last_test_ok,omitempty"`
	LastTestError *string    `json:"last_test_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

func toCredResponse(c *repository.CloudCredential) CloudCredentialResponse {
	return CloudCredentialResponse{
		ID:            c.ID,
		Provider:      c.Provider,
		Name:          c.Name,
		TokenMasked:   maskToken(c.EncryptedToken),
		LastUsedAt:    c.LastUsedAt,
		LastTestAt:    c.LastTestAt,
		LastTestOK:    c.LastTestOK,
		LastTestError: c.LastTestError,
		CreatedAt:     c.CreatedAt,
	}
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return "***"
	}
	return t[:4] + "***" + t[len(t)-4:]
}

func (h *Handler) AdminListCloudCredentials(c *gin.Context) {
	if h.CloudCredentialRepo == nil {
		c.JSON(http.StatusOK, gin.H{"credentials": []any{}})
		return
	}
	rows, err := h.CloudCredentialRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]CloudCredentialResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCredResponse(&r))
	}
	c.JSON(http.StatusOK, gin.H{"credentials": out})
}

type CreateCloudCredentialRequest struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Token    string `json:"token"`
}

func (h *Handler) AdminCreateCloudCredential(c *gin.Context) {
	if h.CloudCredentialRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cloud credentials repo not configured"})
		return
	}
	var req CreateCloudCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.Provider == "" || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider and token required"})
		return
	}
	if req.Name == "" {
		req.Name = req.Provider + "-default"
	}

	// TODO: cipher-encrypt the token via h.CipherService. For now we store
	// as-is so the wiring works end-to-end; flag this in audit log.
	row := &repository.CloudCredential{
		Provider:       req.Provider,
		Name:           req.Name,
		EncryptedToken: req.Token,
	}
	if err := h.CloudCredentialRepo.Create(c.Request.Context(), row); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toCredResponse(row))
}

func (h *Handler) AdminDeleteCloudCredential(c *gin.Context) {
	if h.CloudCredentialRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cloud credentials repo not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.CloudCredentialRepo.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) AdminTestCloudCredential(c *gin.Context) {
	if h.CloudCredentialRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cloud credentials repo not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	cred, err := h.CloudCredentialRepo.Get(c.Request.Context(), id)
	if err != nil || cred == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}
	provider, err := buildProvider(cred)
	if err != nil {
		_ = h.CloudCredentialRepo.UpdateTestResult(c.Request.Context(), id, false, err.Error())
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := provider.Verify(ctx); err != nil {
		_ = h.CloudCredentialRepo.UpdateTestResult(ctx, id, false, err.Error())
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	_ = h.CloudCredentialRepo.UpdateTestResult(ctx, id, true, "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// buildProvider picks the right cloudprovider.Provider implementation for a
// credential row. Today only Hetzner; adding OVH/Vultr means another case.
func buildProvider(c *repository.CloudCredential) (cloudprovider.Provider, error) {
	switch c.Provider {
	case "hetzner":
		return hetzner.New(c.EncryptedToken)
	default:
		return nil, errors.New("unsupported provider: " + c.Provider)
	}
}

// ---------------------------------------------------------------------------
// Provider catalog discovery (used by admin UI dropdowns)
// ---------------------------------------------------------------------------

func (h *Handler) AdminListProviderLocations(c *gin.Context) {
	provider, err := h.providerByName(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	locs, err := provider.Locations(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"locations": locs})
}

func (h *Handler) AdminListProviderServerTypes(c *gin.Context) {
	provider, err := h.providerByName(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	types, err := provider.ServerTypes(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"server_types": types})
}

func (h *Handler) AdminListProviderImages(c *gin.Context) {
	provider, err := h.providerByName(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	images, err := provider.Images(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"images": images})
}

// providerByName resolves a provider name to a Provider client, looking up
// the most recent credential row for that provider.
func (h *Handler) providerByName(c *gin.Context) (cloudprovider.Provider, error) {
	if h.CloudCredentialRepo == nil {
		return nil, errors.New("cloud credentials not configured")
	}
	name := c.Param("provider")
	if name == "" {
		return nil, errors.New("provider path param required")
	}
	cred, err := h.CloudCredentialRepo.GetByProvider(c.Request.Context(), name)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("no credential configured for provider " + name)
	}
	return buildProvider(cred)
}

// ---------------------------------------------------------------------------
// Provisioning templates
// ---------------------------------------------------------------------------

func (h *Handler) AdminListProvisioningTemplates(c *gin.Context) {
	if h.ProvisioningTemplateRepo == nil {
		c.JSON(http.StatusOK, gin.H{"templates": []any{}})
		return
	}
	rows, err := h.ProvisioningTemplateRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": rows})
}

func (h *Handler) AdminGetProvisioningTemplate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	row, err := h.ProvisioningTemplateRepo.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *Handler) AdminCreateProvisioningTemplate(c *gin.Context) {
	if h.ProvisioningTemplateRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template repo not configured"})
		return
	}
	var t repository.ProvisioningTemplate
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return
	}
	if t.Name == "" || t.Provider == "" || t.Location == "" || t.ServerType == "" || t.Tier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, provider, location, server_type, tier required"})
		return
	}
	if t.Image == "" {
		t.Image = "ubuntu-22.04"
	}
	if t.ServerCount == 0 {
		t.ServerCount = 1
	}
	if t.IPv4PerServer == 0 {
		t.IPv4PerServer = 1
	}
	if t.IPv6PerServer == 0 {
		t.IPv6PerServer = 1
	}
	if t.EgressKind == "" {
		t.EgressKind = "cold_smtp"
	}
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	if err := h.ProvisioningTemplateRepo.Create(c.Request.Context(), &t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, t)
}

func (h *Handler) AdminUpdateProvisioningTemplate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var t repository.ProvisioningTemplate
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	t.ID = id
	if err := h.ProvisioningTemplateRepo.Update(c.Request.Context(), &t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *Handler) AdminDeleteProvisioningTemplate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.ProvisioningTemplateRepo.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Provisioning jobs
// ---------------------------------------------------------------------------

type CreateProvisioningJobRequest struct {
	TemplateID  *uuid.UUID      `json:"template_id,omitempty"`
	Custom      json.RawMessage `json:"custom,omitempty"`
	TriggeredBy string          `json:"triggered_by,omitempty"`
}

func (h *Handler) AdminListProvisioningJobs(c *gin.Context) {
	if h.ProvisioningJobRepo == nil {
		c.JSON(http.StatusOK, gin.H{"jobs": []any{}})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	rows, err := h.ProvisioningJobRepo.List(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobs": rows})
}

func (h *Handler) AdminGetProvisioningJob(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	row, err := h.ProvisioningJobRepo.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *Handler) AdminCreateProvisioningJob(c *gin.Context) {
	if h.ProvisioningJobRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "provisioning jobs repo not configured"})
		return
	}
	var req CreateProvisioningJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	// Resolve template or custom config into the JSONB config column.
	var (
		config     json.RawMessage
		templateID *uuid.UUID
		provider   string
	)
	if req.TemplateID != nil {
		if h.ProvisioningTemplateRepo == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template repo not configured"})
			return
		}
		t, err := h.ProvisioningTemplateRepo.Get(c.Request.Context(), *req.TemplateID)
		if err != nil || t == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
			return
		}
		// Snapshot the template into the job's config column.
		b, _ := json.Marshal(t)
		config = b
		templateID = &t.ID
		provider = t.Provider
	} else {
		if len(req.Custom) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "either template_id or custom config required"})
			return
		}
		config = req.Custom
		var stub struct {
			Provider string `json:"provider"`
		}
		_ = json.Unmarshal(req.Custom, &stub)
		provider = stub.Provider
	}

	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider missing from config"})
		return
	}

	triggeredBy := req.TriggeredBy
	if triggeredBy == "" {
		triggeredBy = "admin"
	}

	job := &repository.ProvisioningJob{
		State:       models.ProvJobPending,
		TriggeredBy: triggeredBy,
		Provider:    provider,
		TemplateID:  templateID,
		Config:      config,
	}
	if err := h.ProvisioningJobRepo.Create(c.Request.Context(), job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// The state machine pickup is asynchronous: a background worker on the
	// backend (see internal/app/provisioning Service.Run) processes jobs in
	// state != completed/failed. The admin UI polls GET /admin/provisioning-jobs/:id
	// for live status.

	c.JSON(http.StatusAccepted, job)
}

// ---------------------------------------------------------------------------
// Provisioning policy
// ---------------------------------------------------------------------------

func (h *Handler) AdminListProvisioningPolicy(c *gin.Context) {
	if h.ProvisioningPolicyRepo == nil {
		c.JSON(http.StatusOK, gin.H{"policies": []any{}})
		return
	}
	rows, err := h.ProvisioningPolicyRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"policies": rows})
}

func (h *Handler) AdminUpdateProvisioningPolicy(c *gin.Context) {
	if h.ProvisioningPolicyRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "policy repo not configured"})
		return
	}
	var p repository.ProvisioningPolicy
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if p.Provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider required"})
		return
	}
	if err := h.ProvisioningPolicyRepo.Update(c.Request.Context(), &p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}
