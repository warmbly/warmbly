// Admin endpoints for the SSH-managed worker lifecycle.
//
// Flow:
//   1. Admin POSTs to /admin/workers with host/port/user + name.
//      Backend generates an ed25519 keypair, encrypts the private key via the
//      cipher service under the platform identity, stores the row in
//      `pending` state, and returns the public key to paste into the VPS's
//      ~/.ssh/authorized_keys.
//   2. Admin pastes the pubkey, then POSTs /admin/workers/:id/test.
//      Backend opens an SSH session and runs `true`. First successful connect
//      pins the host fingerprint (TOFU).
//   3. Admin POSTs /admin/workers/:id/install. Backend uploads the project's
//      install-worker.sh + a per-worker env file and runs the installer.
//      install_state moves pending → provisioning → installed.
//   4. Admin can then restart / update / uninstall / rotate-keys / get logs /
//      get a live status snapshot.
//
// The encrypted private key is never returned over the API.

package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/worker_orchestrator"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// DTOs

type adminCreateWorkerRequest struct {
	Name              string `json:"name" binding:"required"`
	Notes             string `json:"notes"`
	WorkerType        string `json:"worker_type" binding:"required,oneof=shared dedicated"`
	FreeTier          bool   `json:"free_tier"`
	SSHHost           string `json:"ssh_host" binding:"required"`
	SSHPort           int    `json:"ssh_port"`
	SSHUser           string `json:"ssh_user"`
	GenerateEnrollURL bool   `json:"generate_enrollment_token"`
}

type adminCreateWorkerResponse struct {
	*models.Worker
	// SSHPublicKey is what the admin pastes into the VPS's authorized_keys.
	SSHPublicKey       string `json:"ssh_public_key"`
	EnrollmentToken    string `json:"enrollment_token,omitempty"`
	EnrollmentTokenTTL int    `json:"enrollment_token_ttl_seconds,omitempty"`
}

// handlers

// AdminCreateWorker creates a new SSH-managed worker.
//
//	POST /admin/workers
func (h *Handler) AdminCreateWorker(c *gin.Context) {
	if h.WorkerOrchestrator == nil || h.WorkerRepo == nil {
		errx.JSON(c, errx.New(errx.Internal, "worker orchestrator not configured"))
		return
	}

	var req adminCreateWorkerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.SSHUser == "" {
		req.SSHUser = "root"
	}

	pub, priv, err := keypairGen()
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to generate keypair"))
		return
	}
	encPriv, err := h.WorkerOrchestrator.EncryptPrivateKey(c.Request.Context(), priv)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to encrypt key"))
		return
	}

	workerID := uuid.New()

	var (
		enrollToken     string
		enrollHash      string
		enrollExpiresAt *time.Time
	)
	if req.GenerateEnrollURL {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			errx.JSON(c, errx.New(errx.Internal, "failed to generate enrollment token"))
			return
		}
		enrollToken = "wmenroll_" + hex.EncodeToString(raw)
		sum := sha256.Sum256([]byte(enrollToken))
		enrollHash = hex.EncodeToString(sum[:])
		exp := time.Now().Add(2 * time.Hour)
		enrollExpiresAt = &exp
	}

	if err := h.WorkerRepo.CreateWorker(c.Request.Context(), repository.CreateWorkerInput{
		ID:                     workerID,
		Name:                   req.Name,
		Notes:                  req.Notes,
		IPAddr:                 req.SSHHost,
		WorkerType:             models.WorkerType(req.WorkerType),
		FreeTier:               req.FreeTier,
		SSHHost:                req.SSHHost,
		SSHPort:                req.SSHPort,
		SSHUser:                req.SSHUser,
		SSHPublicKey:           pub,
		SSHPrivateKeyEncrypted: encPriv,
		EnrollmentTokenHash:    enrollHash,
		EnrollmentTokenExpires: enrollExpiresAt,
	}); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to create worker: "+err.Error()))
		return
	}

	w, xerr := h.fetchWorker(c, workerID)
	if xerr != nil {
		return
	}

	resp := adminCreateWorkerResponse{
		Worker:       w,
		SSHPublicKey: pub,
	}
	if enrollToken != "" {
		resp.EnrollmentToken = enrollToken
		resp.EnrollmentTokenTTL = int(2 * time.Hour / time.Second)
	}
	h.audit(c, models.AuditActionCreate, models.AuditEntityWorker, &workerID, map[string]string{
		"name":     req.Name,
		"ssh_host": req.SSHHost,
		"tier":     req.WorkerType,
	})
	c.JSON(http.StatusCreated, resp)
}

// AdminListSSHWorkers lists all workers with their install state and last_seen.
//
//	GET /admin/workers/managed
func (h *Handler) AdminListSSHWorkers(c *gin.Context) {
	if h.WorkerRepo == nil {
		errx.JSON(c, errx.New(errx.Internal, "worker repo not configured"))
		return
	}
	workers, err := h.WorkerRepo.ListWorkersDetail(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	// Hydrate tags in one round-trip so the UI gets them in the list response.
	ptrs := make([]*models.Worker, len(workers))
	for i := range workers {
		ptrs[i] = &workers[i]
	}
	_ = h.WorkerRepo.HydrateWorkerTags(c.Request.Context(), ptrs)
	c.JSON(http.StatusOK, gin.H{"data": workers})
}

// AdminGetSSHWorker returns full worker detail.
//
//	GET /admin/workers/:id/managed
func (h *Handler) AdminGetSSHWorker(c *gin.Context) {
	w, xerr := h.parseAndFetch(c)
	if xerr != nil {
		return
	}
	tags, err := h.WorkerRepo.GetWorkerTags(c.Request.Context(), w.ID)
	if err == nil {
		w.Tags = tags
	}
	c.JSON(http.StatusOK, w)
}

type setTagsBody struct {
	Tags []string `json:"tags"`
}

// AdminSetWorkerTags replaces a worker's tag set.
//
//	PUT /admin/workers/:id/tags
func (h *Handler) AdminSetWorkerTags(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	var body setTagsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	// Normalize: trim, lowercase, dedupe, drop empties. The DB constraint
	// rejects garbage tags too; this is just to avoid 500s on the happy path.
	seen := map[string]struct{}{}
	tags := make([]string, 0, len(body.Tags))
	for _, t := range body.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		tags = append(tags, t)
	}
	if err := h.WorkerRepo.SetWorkerTags(c.Request.Context(), id, tags); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.audit(c, models.AuditActionUpdate, models.AuditEntityWorker, &id, map[string]string{
		"tags": strings.Join(tags, ","),
	})
	c.JSON(http.StatusOK, gin.H{"ok": true, "tags": tags})
}

// AdminListWorkerTags returns every distinct tag in use, for autocomplete.
//
//	GET /admin/workers/tags
func (h *Handler) AdminListWorkerTags(c *gin.Context) {
	tags, err := h.WorkerRepo.ListAllWorkerTags(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tags})
}

// AdminTestWorker runs a no-op SSH command. Pins the host fingerprint on
// first success.
//
//	POST /admin/workers/:id/test
func (h *Handler) AdminTestWorker(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.TestConnection(c.Request.Context(), id); err != nil {
		h.audit(c, models.AuditActionTest, models.AuditEntityWorker, &id, map[string]string{"ok": "false", "error": err.Error()})
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	h.audit(c, models.AuditActionTest, models.AuditEntityWorker, &id, map[string]string{"ok": "true"})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AdminInstallWorker uploads installer + env file and runs it.
//
//	POST /admin/workers/:id/install
func (h *Handler) AdminInstallWorker(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.Install(c.Request.Context(), id); err != nil {
		h.audit(c, models.AuditActionInstall, models.AuditEntityWorker, &id, map[string]string{"ok": "false", "error": err.Error()})
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionInstall, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminRestartWorker(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.Restart(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionRestart, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminUpdateWorkerImage(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.Update(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionUpgrade, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminUninstallWorker(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.Uninstall(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionUninstall, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminWorkerStatusLive(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	st, err := h.WorkerOrchestrator.Status(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *Handler) AdminWorkerLogs(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	lines, _ := strconv.Atoi(c.Query("lines"))
	logs, err := h.WorkerOrchestrator.TailLogs(c.Request.Context(), id, lines)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

func (h *Handler) AdminRotateWorkerKeys(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	newPub, err := h.WorkerOrchestrator.RotateKeys(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionRotateKeys, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ssh_public_key": newPub})
}

func (h *Handler) AdminSystemUpdate(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	r, err := h.WorkerOrchestrator.SystemUpdate(c.Request.Context(), id)
	if err != nil {
		// Return the partial output to the client so they can see what failed
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	meta := map[string]string{}
	if r != nil && r.RebootRequired {
		meta["reboot_required"] = "true"
	}
	h.audit(c, models.AuditActionSystemUpdate, models.AuditEntityWorker, &id, meta)
	c.JSON(http.StatusOK, r)
}

func (h *Handler) AdminRebootWorker(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.WorkerOrchestrator.RebootWorker(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionReboot, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AdminConvertWorkerToDedicated drains a shared worker's accounts to a
// target worker, flips its worker_type to dedicated, and binds it to a
// user/org via dedicated_worker_assignments.
//
// Body:
//
//	{
//	  "user_id":         "uuid",           // the org/user that gets exclusive use
//	  "subscription_id": "uuid",           // their active sub
//	  "drain_to_worker_id": "uuid|null"    // optional: target for evicted accounts.
//	                                       //   null = let assignment service pick
//	                                       //   per-account
//	}
//
// The whole operation is sequential, not transactional across services —
// if a step fails mid-flight the worker is left in a half-converted state
// and the admin needs to investigate. That's acceptable because the steps
// are individually idempotent: re-running the endpoint with the same
// inputs converges.
type convertToDedicatedBody struct {
	UserID          string  `json:"user_id" binding:"required"`
	SubscriptionID  string  `json:"subscription_id" binding:"required"`
	DrainToWorkerID *string `json:"drain_to_worker_id"`
}

func (h *Handler) AdminConvertWorkerToDedicated(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}

	var body convertToDedicatedBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	userID, err := uuid.Parse(body.UserID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user_id"))
		return
	}
	subID, err := uuid.Parse(body.SubscriptionID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid subscription_id"))
		return
	}

	w, err := h.WorkerRepo.GetWorkerDetail(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if w == nil {
		errx.JSON(c, errx.New(errx.NotFound, "worker not found"))
		return
	}
	if w.WorkerType == models.WorkerTypeDedicated {
		errx.JSON(c, errx.New(errx.BadRequest, "worker is already dedicated"))
		return
	}

	// Step 1: drain existing accounts to a target. If drain_to_worker_id is
	// supplied, move them all there; otherwise we leave reassignment to the
	// assignment service which picks per-account based on the source org.
	accountIDs, err := h.WorkerRepo.GetEmailAccountsByWorkerID(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "list accounts: "+err.Error()))
		return
	}
	movedTo := ""
	if body.DrainToWorkerID != nil && *body.DrainToWorkerID != "" {
		targetID, perr := uuid.Parse(*body.DrainToWorkerID)
		if perr != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid drain_to_worker_id"))
			return
		}
		if targetID == id {
			errx.JSON(c, errx.New(errx.BadRequest, "drain target must be a different worker"))
			return
		}
		for _, aid := range accountIDs {
			if err := h.WorkerRepo.UpdateEmailAccountWorker(c.Request.Context(), aid, targetID); err != nil {
				errx.JSON(c, errx.New(errx.Internal, "drain "+aid.String()+": "+err.Error()))
				return
			}
			_ = h.WorkerRepo.IncrementAccountCount(c.Request.Context(), targetID)
			_ = h.WorkerRepo.DecrementAccountCount(c.Request.Context(), id)
		}
		movedTo = targetID.String()
	} else if len(accountIDs) > 0 {
		// We don't auto-pick targets here because the right per-account choice
		// depends on each account's owning org. If the admin wants that, they
		// should pick a single drain target.
		errx.JSON(c, errx.New(errx.BadRequest, "worker has accounts; supply drain_to_worker_id to evict them first"))
		return
	}

	// Step 2: flip worker_type to dedicated.
	if err := h.WorkerRepo.SetWorkerType(c.Request.Context(), id, models.WorkerTypeDedicated); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "set type: "+err.Error()))
		return
	}

	// Step 3: bind to user/org. Idempotent via CreateDedicatedAssignmentIfNotExists.
	created, err := h.WorkerRepo.CreateDedicatedAssignmentIfNotExists(c.Request.Context(), &models.DedicatedWorkerAssignment{
		ID:             uuid.New(),
		WorkerID:       id,
		UserID:         userID,
		SubscriptionID: subID,
		AssignedAt:     time.Now(),
	})
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "create assignment: "+err.Error()))
		return
	}

	h.audit(c, "convert_to_dedicated", models.AuditEntityWorker, &id, map[string]string{
		"user_id":         userID.String(),
		"subscription_id": subID.String(),
		"drained_to":      movedTo,
		"accounts_moved":  itoa(len(accountIDs)),
		"new_assignment":  boolStr(created),
	})
	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"accounts_drained": len(accountIDs),
		"new_assignment":   created,
	})
}

func itoa(n int) string { return strconv.Itoa(n) }
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type preflightBody struct {
	Host string `json:"host" binding:"required"`
	Port int    `json:"port"`
}

type preflightResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AdminPreflightWorker checks TCP reachability of host:port before the
// admin commits to creating a worker row. Catches typos and firewall
// problems while the wizard is still open — much better UX than failing
// later at the SSH test step.
//
// Note: this does NOT attempt SSH handshake. We don't have credentials at
// this stage. A successful TCP dial just means "something is listening
// there"; the SSH test runs after the worker is created and the admin has
// pasted the generated pubkey.
func (h *Handler) AdminPreflightWorker(c *gin.Context) {
	var body preflightBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	port := body.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(body.Host, strconv.Itoa(port))

	start := time.Now()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(c.Request.Context(), "tcp", addr)
	if err != nil {
		c.JSON(http.StatusOK, preflightResult{OK: false, Error: err.Error()})
		return
	}
	_ = conn.Close()
	c.JSON(http.StatusOK, preflightResult{OK: true, LatencyMS: time.Since(start).Milliseconds()})
}

type setRiskPoolBody struct {
	RiskPool string `json:"risk_pool" binding:"required,oneof=clean risky quarantine"`
}

// AdminSetWorkerRiskPool moves a shared worker into a different risk pool.
// The rebalancer will redistribute mailboxes on the next tick — admins
// don't need to migrate accounts manually after this.
func (h *Handler) AdminSetWorkerRiskPool(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	var body setRiskPoolBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	if err := h.WorkerRepo.SetWorkerRiskPool(c.Request.Context(), id, models.WorkerRiskPool(body.RiskPool)); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionUpdate, models.AuditEntityWorker, &id, map[string]string{
		"risk_pool": body.RiskPool,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminDeleteSSHWorker(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	// Best-effort uninstall first. Ignore errors — the row deletion happens
	// regardless so an unreachable worker doesn't leave orphan records.
	_ = h.WorkerOrchestrator.Uninstall(c.Request.Context(), id)
	if err := h.WorkerRepo.DeleteWorker(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionDelete, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// helpers

func (h *Handler) parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid worker ID"))
		return uuid.Nil, false
	}
	return id, true
}

// audit fires an admin audit log entry. Writes to admin_audit_log (the
// table the /admin/audit-logs viewer queries) so every action on workers,
// credentials, and releases is browsable alongside ban/unban/etc.
//
// Fire-and-forget — AdminService spawns its own goroutine so the response
// is never blocked. Safe to call with nil entityID and/or nil metadata.
func (h *Handler) audit(c *gin.Context, action models.AuditAction, entity models.AuditEntityType, entityID *uuid.UUID, metadata map[string]string) {
	if h.AdminService == nil {
		return
	}
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		return
	}
	var details map[string]any
	if len(metadata) > 0 {
		details = make(map[string]any, len(metadata))
		for k, v := range metadata {
			details[k] = v
		}
	}
	h.AdminService.LogAdminAction(
		c.Request.Context(),
		*adminID,
		string(action),
		string(entity),
		entityID,
		details,
		c.ClientIP(),
		c.GetHeader("User-Agent"),
	)
}

func (h *Handler) parseAndFetch(c *gin.Context) (*models.Worker, error) {
	id, ok := h.parseID(c)
	if !ok {
		return nil, errors.New("bad id")
	}
	return h.fetchWorker(c, id)
}

func (h *Handler) fetchWorker(c *gin.Context, id uuid.UUID) (*models.Worker, error) {
	w, err := h.WorkerRepo.GetWorkerDetail(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return nil, err
	}
	if w == nil {
		errx.JSON(c, errx.New(errx.NotFound, "worker not found"))
		return nil, errors.New("not found")
	}
	return w, nil
}

func keypairGen() (pub, priv string, err error) {
	return worker_orchestrator.GenerateKeypair()
}
