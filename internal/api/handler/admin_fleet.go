package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Fleet placement as the operator sees it: every worker against its capacity
// row, the decision log the control loops write, and the dedicated bindings.

// AdminFleetCapacity lists every worker with its capacity-view row, hottest first.
//
// GET /admin/fleet/capacity
func (h *Handler) AdminFleetCapacity(c *gin.Context) {
	if h.AdminFleetRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet view is not available on this instance"))
		return
	}
	rows, err := h.AdminFleetRepo.Capacity(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// AdminFleetDecisions lists decision_log newest first.
//
// GET /admin/fleet/decisions?kind=&worker_id=&limit=
func (h *Handler) AdminFleetDecisions(c *gin.Context) {
	if h.AdminFleetRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet view is not available on this instance"))
		return
	}
	var workerID *uuid.UUID
	if raw := c.Query("worker_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid worker_id"))
			return
		}
		workerID = &id
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid limit"))
			return
		}
		limit = n
	}
	rows, err := h.AdminFleetRepo.Decisions(c.Request.Context(), c.Query("kind"), workerID, limit)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// AdminFleetDedicated lists active worker-to-organization bindings.
//
// GET /admin/fleet/dedicated
func (h *Handler) AdminFleetDedicated(c *gin.Context) {
	if h.AdminFleetRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet view is not available on this instance"))
		return
	}
	rows, err := h.AdminFleetRepo.DedicatedAssignments(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// AdminFleetReleaseDedicated is the inverse of AdminConvertWorkerToDedicated:
// the org's mailboxes go back to shared premium workers, the binding is
// released, and the worker re-enters the shared pool once nothing binds it.
//
// POST /admin/fleet/dedicated/:orgId/release
func (h *Handler) AdminFleetReleaseDedicated(c *gin.Context) {
	orgID, ok := parseUUIDParam(c, "orgId")
	if !ok {
		return
	}
	if h.WorkerAssignmentService == nil || h.WorkerRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "worker placement is not available on this instance"))
		return
	}
	ctx := c.Request.Context()

	assignment, err := h.WorkerRepo.GetActiveDedicatedAssignment(ctx, orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if assignment == nil {
		errx.JSON(c, errx.New(errx.NotFound, "organization has no active dedicated worker"))
		return
	}
	workerID := assignment.WorkerID

	before, err := h.WorkerRepo.GetEmailAccountsByWorkerID(ctx, workerID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "list accounts: "+err.Error()))
		return
	}

	// Moves every org mailbox onto a live shared premium worker and releases
	// the binding; a mailbox with no shared target stays put rather than failing.
	if err := h.WorkerAssignmentService.MigrateOrgToShared(ctx, orgID); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "migrate to shared: "+err.Error()))
		return
	}
	// MigrateOrgToShared swallows its own release error, so release again; the
	// UPDATE is a no-op when the row is already released.
	if err := h.WorkerAssignmentService.ReleaseDedicatedWorker(ctx, orgID); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "release assignment: "+err.Error()))
		return
	}

	after, err := h.WorkerRepo.GetEmailAccountsByWorkerID(ctx, workerID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "list accounts: "+err.Error()))
		return
	}
	moved := len(before) - len(after)
	if moved < 0 {
		moved = 0
	}

	// The worker only returns to the shared pool when no other org binds it.
	stillBound := false
	if h.AdminFleetRepo != nil {
		active, err := h.AdminFleetRepo.DedicatedAssignments(ctx)
		if err != nil {
			errx.JSON(c, errx.New(errx.Internal, "list assignments: "+err.Error()))
			return
		}
		for _, a := range active {
			if a.WorkerID == workerID {
				stillBound = true
				break
			}
		}
	}
	returnedToShared := false
	if !stillBound {
		if err := h.WorkerRepo.SetWorkerType(ctx, workerID, models.WorkerTypeShared); err != nil {
			errx.JSON(c, errx.New(errx.Internal, "set type: "+err.Error()))
			return
		}
		returnedToShared = true
	}

	h.audit(c, "release_dedicated", models.AuditEntityWorker, &workerID, map[string]string{
		"organization_id":    orgID.String(),
		"subscription_id":    assignment.SubscriptionID.String(),
		"assignment_id":      assignment.ID.String(),
		"accounts_moved":     itoa(moved),
		"accounts_remaining": itoa(len(after)),
		"returned_to_shared": boolStr(returnedToShared),
	})
	c.JSON(http.StatusOK, gin.H{
		"ok":                 true,
		"worker_id":          workerID,
		"accounts_moved":     moved,
		"accounts_remaining": len(after),
		"returned_to_shared": returnedToShared,
	})
}
