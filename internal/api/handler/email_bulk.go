package handler

import (
	"context"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// BulkMailboxMax bounds one bulk mailbox call, matching the unibox id caps.
const BulkMailboxMax = 500

type bulkMailboxRequest struct {
	EmailIDs []string `json:"email_ids" binding:"required"`
}

type bulkWarmupRequest struct {
	EmailIDs []string `json:"email_ids" binding:"required"`
	Action   string   `json:"action" binding:"required"`
}

type bulkStatusRequest struct {
	EmailIDs []string `json:"email_ids" binding:"required"`
	Status   string   `json:"status" binding:"required"`
}

// BulkMailboxResult is one mailbox's outcome; code and message are what the single route would answer.
type BulkMailboxResult struct {
	ID      uuid.UUID `json:"id"`
	OK      bool      `json:"ok"`
	Code    string    `json:"code,omitempty"`
	Message string    `json:"message,omitempty"`
}

type bulkMailboxResponse struct {
	Succeeded int                 `json:"succeeded"`
	Failed    int                 `json:"failed"`
	Results   []BulkMailboxResult `json:"results"`
}

// bulkMailboxIDs parses and dedupes the ids and holds them to the caller's mailbox allowlist, as RequireEmailAccountParam does per id.
func bulkMailboxIDs(c *gin.Context, raw []string) ([]uuid.UUID, *errx.Error) {
	if len(raw) == 0 {
		return nil, errx.New(errx.BadRequest, "email_ids must name at least one mailbox.")
	}
	if len(raw) > BulkMailboxMax {
		return nil, errx.New(errx.BadRequest, "email_ids can name at most "+strconv.Itoa(BulkMailboxMax)+" mailboxes.")
	}
	out := make([]uuid.UUID, 0, len(raw))
	seen := make(map[uuid.UUID]struct{}, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, errx.ErrUuid
		}
		if _, dup := seen[id]; dup {
			continue
		}
		if !middleware.EmailAccountAllowed(c, id) {
			return nil, middleware.ErrEmailAccountNotAllowed(c)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// runBulkMailboxes applies one single-mailbox operation to every id, a few at a time, and answers with each outcome.
func (h *Handler) runBulkMailboxes(c *gin.Context, ids []uuid.UUID, apply func(ctx context.Context, id uuid.UUID) *errx.Error) bulkMailboxResponse {
	// Each operation publishes side effects, so a dropped connection does not abandon a batch half way.
	ctx := context.WithoutCancel(c.Request.Context())
	res := bulkMailboxResponse{Results: make([]BulkMailboxResult, len(ids))}
	var wg sync.WaitGroup
	sem := make(chan struct{}, config.MailboxBulkConcurrency)
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id uuid.UUID) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := BulkMailboxResult{ID: id, OK: true}
			if xerr := apply(ctx, id); xerr != nil {
				r.OK, r.Code, r.Message = false, xerr.ResponseCode(), bulkRowMessage(xerr)
			}
			res.Results[i] = r
		}(i, id)
	}
	wg.Wait()
	for _, r := range res.Results {
		if r.OK {
			res.Succeeded++
		} else {
			res.Failed++
		}
	}
	return res
}

// bulkRowMessage is what a row may say: a server-class detail stays in the log, as errx.JSON keeps it.
func bulkRowMessage(xerr *errx.Error) string {
	if xerr.Code == errx.Internal && !xerr.Public {
		return "Something went wrong."
	}
	return xerr.Message
}

func bulkAuditMeta(kind string, res bulkMailboxResponse) map[string]string {
	return map[string]string{
		"bulk":      kind,
		"succeeded": strconv.Itoa(res.Succeeded),
		"failed":    strconv.Itoa(res.Failed),
	}
}

// BulkDeleteEmails removes many mailboxes through the single delete path.
// Retrying is safe: a mailbox already removed answers not_found in its row.
// POST /emails/bulk/delete
func (h *Handler) BulkDeleteEmails(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	var req bulkMailboxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	ids, xerr := bulkMailboxIDs(c, req.EmailIDs)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	res := h.runBulkMailboxes(c, ids, func(ctx context.Context, id uuid.UUID) *errx.Error {
		return h.EmailService.Delete(ctx, orgID.String(), id.String())
	})
	if res.Succeeded > 0 {
		h.auditOrg(c, models.AuditActionDelete, models.AuditEntityEmailAccount, nil, nil, bulkAuditMeta("delete", res))
	}
	c.JSON(http.StatusOK, res)
}

// BulkWarmupEmails starts, pauses, resumes or stops warmup on many mailboxes.
// Each action sets a state, so a retry converges on the same result.
// POST /emails/bulk/warmup
func (h *Handler) BulkWarmupEmails(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	var req bulkWarmupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	switch req.Action {
	case "start", "pause", "resume", "stop":
	default:
		errx.Handle(c, errx.New(errx.BadRequest, "action must be start, pause, resume or stop."))
		return
	}
	ids, xerr := bulkMailboxIDs(c, req.EmailIDs)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	res := h.runBulkMailboxes(c, ids, func(ctx context.Context, id uuid.UUID) *errx.Error {
		if _, xerr := h.EmailService.SetWarmupLifecycle(ctx, orgID.String(), id.String(), req.Action); xerr != nil {
			return xerr
		}
		// Warming starts now rather than on the next reconciler pass, as on the single route.
		if req.Action == "start" || req.Action == "resume" {
			_ = h.TasksService.EnsureWarmupScheduled(ctx, id)
		}
		return nil
	})
	if res.Succeeded > 0 {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, nil, map[string]string{"warmup": req.Action}, bulkAuditMeta("warmup", res))
	}
	c.JSON(http.StatusOK, res)
}

// BulkStatusEmails switches many mailboxes on or off.
// Each call sets a state, so a retry converges on the same result.
// POST /emails/bulk/status
func (h *Handler) BulkStatusEmails(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	var req bulkStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	if req.Status != "active" && req.Status != "inactive" {
		errx.Handle(c, errx.New(errx.BadRequest, "status must be active or inactive."))
		return
	}
	ids, xerr := bulkMailboxIDs(c, req.EmailIDs)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	userID := middleware.GetUserID(c)
	res := h.runBulkMailboxes(c, ids, func(ctx context.Context, id uuid.UUID) *errx.Error {
		status := req.Status
		if _, xerr := h.EmailService.Update(ctx, orgID.String(), userID, id.String(), &models.UpdateEmail{Status: &status}); xerr != nil {
			return xerr
		}
		// A mailbox switched back on resumes its warmup now, as on PATCH /emails/:id.
		if status == "active" {
			_ = h.TasksService.EnsureWarmupScheduled(ctx, id)
		}
		return nil
	})
	if res.Succeeded > 0 {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, nil, map[string]string{"status": req.Status}, bulkAuditMeta("status", res))
	}
	c.JSON(http.StatusOK, res)
}
