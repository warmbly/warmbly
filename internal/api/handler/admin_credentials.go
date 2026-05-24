// Admin endpoints for reusable worker credentials.
//
//   /admin/aws-credentials             list / create
//   /admin/aws-credentials/:id         get / update / delete
//   /admin/worker-profiles             list / create
//   /admin/worker-profiles/:id         get / update / delete
//   /admin/worker-profiles/:id/workers list workers using this profile
//   /admin/worker-profiles/:id/apply   re-write env + restart on every assigned worker
//   /admin/workers/:id/profile         assign / unassign a profile to a worker
//   /admin/workers/:id/apply           re-write env + restart for a single worker
//
// Secret material is never returned over the API. Update bodies use empty
// strings to mean "keep the stored value as-is". The dashboard renders set
// secrets as "••••••".

package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// AWS credentials

type awsCredsBody struct {
	Name            string `json:"name" binding:"required"`
	Description     string `json:"description"`
	Region          string `json:"region" binding:"required"`
	AccessKeyID     string `json:"access_key_id" binding:"required"`
	SecretAccessKey string `json:"secret_access_key"` // empty on update = keep
}

func (h *Handler) AdminListAWSCreds(c *gin.Context) {
	if h.CredentialsRepo == nil {
		errx.JSON(c, errx.New(errx.Internal, "credentials repo not configured"))
		return
	}
	creds, err := h.CredentialsRepo.ListAWSCreds(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": creds})
}

func (h *Handler) AdminCreateAWSCreds(c *gin.Context) {
	var body awsCredsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	if body.SecretAccessKey == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "secret_access_key is required on create"))
		return
	}
	enc, err := h.WorkerOrchestrator.EncryptSecret(c.Request.Context(), body.SecretAccessKey)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "encrypt: "+err.Error()))
		return
	}
	id, err := h.CredentialsRepo.CreateAWSCreds(c.Request.Context(), repository.CreateAWSCredsInput{
		Name:                     body.Name,
		Description:              body.Description,
		Region:                   body.Region,
		AccessKeyID:              body.AccessKeyID,
		SecretAccessKeyEncrypted: enc,
	})
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionCreate, models.AuditEntityAWSCredentials, &id, map[string]string{
		"name":   body.Name,
		"region": body.Region,
	})
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) AdminGetAWSCreds(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	creds, err := h.CredentialsRepo.GetAWSCreds(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if creds == nil {
		errx.JSON(c, errx.New(errx.NotFound, "credentials not found"))
		return
	}
	// Don't leak ciphertext.
	creds.SecretAccessKeyEncrypted = ""
	c.JSON(http.StatusOK, creds)
}

func (h *Handler) AdminUpdateAWSCreds(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body awsCredsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	enc := ""
	if body.SecretAccessKey != "" {
		var err error
		enc, err = h.WorkerOrchestrator.EncryptSecret(c.Request.Context(), body.SecretAccessKey)
		if err != nil {
			errx.JSON(c, errx.New(errx.Internal, "encrypt: "+err.Error()))
			return
		}
	}
	if err := h.CredentialsRepo.UpdateAWSCreds(c.Request.Context(), id, repository.UpdateAWSCredsInput{
		Name:                     body.Name,
		Description:              body.Description,
		Region:                   body.Region,
		AccessKeyID:              body.AccessKeyID,
		SecretAccessKeyEncrypted: enc,
	}); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	changes := map[string]string{}
	if enc != "" {
		changes["secret_access_key"] = "rotated"
	}
	h.audit(c, models.AuditActionUpdate, models.AuditEntityAWSCredentials, &id, changes)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminDeleteAWSCreds(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if err := h.CredentialsRepo.DeleteAWSCreds(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionDelete, models.AuditEntityAWSCredentials, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// worker profiles

type profileBody struct {
	Name                 string  `json:"name" binding:"required"`
	Description          string  `json:"description"`
	AppEnv               string  `json:"app_env"`
	WorkerImage          string  `json:"worker_image"`
	KafkaBootstrap       string  `json:"kafka_bootstrap_servers"`
	KafkaSASLUsername    string  `json:"kafka_sasl_username"`
	KafkaSASLPassword    string  `json:"kafka_sasl_password"`
	SchemaRegistryURL    string  `json:"schema_registry_url"`
	SchemaRegistryKey    string  `json:"schema_registry_key"`
	SchemaRegistrySecret string  `json:"schema_registry_secret"`
	RedisURL             string  `json:"redis_url"`
	AWSCredentialID      *string `json:"aws_credential_id"`
}

func (h *Handler) AdminListProfiles(c *gin.Context) {
	if h.CredentialsRepo == nil {
		errx.JSON(c, errx.New(errx.Internal, "credentials repo not configured"))
		return
	}
	profiles, err := h.CredentialsRepo.ListProfiles(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profiles})
}

func (h *Handler) AdminCreateProfile(c *gin.Context) {
	var body profileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	in, xerr := h.profileBodyToInput(c, body)
	if xerr {
		return
	}
	id, err := h.CredentialsRepo.CreateProfile(c.Request.Context(), in)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionCreate, models.AuditEntityWorkerProfile, &id, map[string]string{
		"name":    body.Name,
		"app_env": body.AppEnv,
	})
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) AdminGetProfile(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	p, err := h.CredentialsRepo.GetProfile(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if p == nil {
		errx.JSON(c, errx.New(errx.NotFound, "profile not found"))
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) AdminUpdateProfile(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body profileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	in, xerr := h.profileBodyToInput(c, body)
	if xerr {
		return
	}
	if err := h.CredentialsRepo.UpdateProfile(c.Request.Context(), id, in); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	// Record which secret fields were rotated (we don't log the values, only that they changed).
	changes := map[string]string{}
	if body.KafkaSASLPassword != "" {
		changes["kafka_sasl_password"] = "rotated"
	}
	if body.SchemaRegistrySecret != "" {
		changes["schema_registry_secret"] = "rotated"
	}
	if body.RedisURL != "" {
		changes["redis_url"] = "rotated"
	}
	h.audit(c, models.AuditActionUpdate, models.AuditEntityWorkerProfile, &id, changes)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminDeleteProfile(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if err := h.CredentialsRepo.DeleteProfile(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionDelete, models.AuditEntityWorkerProfile, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminListProfileWorkers(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	workers, err := h.WorkerRepo.ListWorkersByProfile(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": workers})
}

// AdminApplyProfile re-applies the profile to every assigned worker by
// re-writing /etc/warmbly/worker.env and restarting the service.
//
// Reports a per-worker outcome map so the UI can show what succeeded.
func (h *Handler) AdminApplyProfile(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	workers, err := h.WorkerRepo.ListWorkersByProfile(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	results := make([]gin.H, 0, len(workers))
	for _, w := range workers {
		// Skip workers that aren't installed yet — apply is restart-based.
		if w.InstallState != models.WorkerInstallStateInstalled {
			results = append(results, gin.H{"worker_id": w.ID, "ok": false, "skipped": "not installed"})
			continue
		}
		applyErr := h.WorkerOrchestrator.ApplyConfig(c.Request.Context(), w.ID)
		r := gin.H{"worker_id": w.ID, "ok": applyErr == nil}
		if applyErr != nil {
			r["error"] = applyErr.Error()
		}
		results = append(results, r)
	}
	okCount := 0
	for _, r := range results {
		if v, _ := r["ok"].(bool); v {
			okCount++
		}
	}
	h.audit(c, models.AuditActionApply, models.AuditEntityWorkerProfile, &id, map[string]string{
		"workers_applied": fmt.Sprintf("%d/%d", okCount, len(results)),
	})
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// worker → profile binding

type assignProfileBody struct {
	ProfileID *string `json:"profile_id"`
}

func (h *Handler) AdminAssignWorkerProfile(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body assignProfileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	var pid *uuid.UUID
	if body.ProfileID != nil && *body.ProfileID != "" {
		parsed, err := uuid.Parse(*body.ProfileID)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid profile_id"))
			return
		}
		pid = &parsed
	}
	if err := h.WorkerRepo.AssignWorkerProfile(c.Request.Context(), id, pid); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	meta := map[string]string{}
	if pid != nil {
		meta["profile_id"] = pid.String()
	} else {
		meta["profile_id"] = "(none)"
	}
	h.audit(c, models.AuditActionAssign, models.AuditEntityWorker, &id, meta)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AdminApplyWorkerConfig re-writes env and restarts the service for ONE
// worker. Useful when the admin wants to pick up the latest profile values
// without re-running the full installer.
func (h *Handler) AdminApplyWorkerConfig(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.ApplyConfig(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionApply, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// helpers

func (h *Handler) profileBodyToInput(c *gin.Context, body profileBody) (repository.CreateProfileInput, bool) {
	ctx := c.Request.Context()
	encrypt := func(s string) (string, bool) {
		if s == "" {
			return "", true
		}
		enc, err := h.WorkerOrchestrator.EncryptSecret(ctx, s)
		if err != nil {
			errx.JSON(c, errx.New(errx.Internal, "encrypt: "+err.Error()))
			return "", false
		}
		return enc, true
	}

	kafkaEnc, ok := encrypt(body.KafkaSASLPassword)
	if !ok {
		return repository.CreateProfileInput{}, true
	}
	schemaEnc, ok := encrypt(body.SchemaRegistrySecret)
	if !ok {
		return repository.CreateProfileInput{}, true
	}
	redisEnc, ok := encrypt(body.RedisURL)
	if !ok {
		return repository.CreateProfileInput{}, true
	}

	var awsID *uuid.UUID
	if body.AWSCredentialID != nil && *body.AWSCredentialID != "" {
		parsed, err := uuid.Parse(*body.AWSCredentialID)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid aws_credential_id"))
			return repository.CreateProfileInput{}, true
		}
		awsID = &parsed
	}

	appEnv := body.AppEnv
	if appEnv == "" {
		appEnv = "prod"
	}
	image := body.WorkerImage
	if image == "" {
		image = "ghcr.io/warmbly/worker:latest"
	}

	return repository.CreateProfileInput{
		Name:                          body.Name,
		Description:                   body.Description,
		AppEnv:                        appEnv,
		WorkerImage:                   image,
		KafkaBootstrap:                body.KafkaBootstrap,
		KafkaSASLUsername:             body.KafkaSASLUsername,
		KafkaSASLPasswordEncrypted:    kafkaEnc,
		SchemaRegistryURL:             body.SchemaRegistryURL,
		SchemaRegistryKey:             body.SchemaRegistryKey,
		SchemaRegistrySecretEncrypted: schemaEnc,
		RedisURLEncrypted:             redisEnc,
		AWSCredentialID:               awsID,
	}, false
}

func parseUUID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid "+param))
		return uuid.Nil, false
	}
	return id, true
}
