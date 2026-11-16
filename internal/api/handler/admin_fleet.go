package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/fleetscale"
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

// AdminFleetReleaseIsolatedEgress releases an organization's reserved worker
// back to the fleet.
//
// It only deletes the binding. Nothing migrates: the worker carries no
// category to reset, and the org's mailboxes stay where they are until the
// rotation loop finds them a better home on its own schedule. Forcing them to
// move here would re-authenticate every one of them from a new address for no
// deliverability gain.
//
// POST /admin/fleet/dedicated/:orgId/release
func (h *Handler) AdminFleetReleaseIsolatedEgress(c *gin.Context) {
	orgID, ok := parseUUIDParam(c, "orgId")
	if !ok {
		return
	}
	if h.WorkerRepo == nil {
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
		errx.JSON(c, errx.New(errx.NotFound, "organization has no reserved worker"))
		return
	}
	workerID := assignment.WorkerID

	// Release the exact row read above by id, so a reservation created
	// meanwhile is never touched.
	released, err := h.WorkerRepo.ReleaseDedicatedAssignmentByID(ctx, assignment.ID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "release assignment: "+err.Error()))
		return
	}

	remaining, err := h.WorkerRepo.GetEmailAccountsByWorkerID(ctx, workerID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "list accounts: "+err.Error()))
		return
	}

	h.audit(c, "release_isolated_egress", models.AuditEntityWorker, &workerID, map[string]string{
		"organization_id":    orgID.String(),
		"subscription_id":    assignment.SubscriptionID.String(),
		"assignment_id":      assignment.ID.String(),
		"accounts_remaining": itoa(len(remaining)),
	})
	c.JSON(http.StatusOK, gin.H{
		"ok":                 released,
		"worker_id":          workerID,
		"accounts_remaining": len(remaining),
	})
}

// AdminFleetScale reports the worker Auto Scaling Group's size against its cap.
//
// GET /admin/fleet/scale
func (h *Handler) AdminFleetScale(c *gin.Context) {
	if h.FleetScale == nil {
		errx.JSON(c, fleetScaleNotConfigured())
		return
	}
	st, err := h.FleetScale.Status(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, st)
}

type setFleetMaxBody struct {
	Max int `json:"max" binding:"required"`
}

// AdminFleetSetMax moves the cap, between the running fleet and the group's ceiling.
//
// PATCH /admin/fleet/scale
func (h *Handler) AdminFleetSetMax(c *gin.Context) {
	if h.FleetScale == nil {
		errx.JSON(c, fleetScaleNotConfigured())
		return
	}
	var body setFleetMaxBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	before, err := h.FleetScale.Status(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	st, err := h.FleetScale.SetMax(c.Request.Context(), body.Max)
	switch {
	case errors.Is(err, fleetscale.ErrOutOfRange):
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "fleet_max_out_of_range", err.Error()))
		return
	case errors.Is(err, fleetscale.ErrNoCeiling):
		errx.JSON(c, errx.NewWithIdentifier(errx.Conflict, "fleet_max_ceiling_missing", "The worker Auto Scaling Group has no "+fleetscale.CeilingTag+" tag, so its cap cannot change here. Apply the workers Terraform root to set it."))
		return
	case err != nil:
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, "fleet_max_set", models.AuditEntityWorker, nil, map[string]string{
		"group": st.Group,
		"from":  strconv.Itoa(before.Max),
		"to":    strconv.Itoa(st.Max),
	})
	c.JSON(http.StatusOK, st)
}

func fleetScaleNotConfigured() *errx.Error {
	return errx.NewWithIdentifier(errx.ServiceUnavailable, "fleet_autoscaling_not_configured", "This instance does not manage a worker Auto Scaling Group. Set FLEET_AUTOSCALING_GROUP on the backend to manage its cap here.")
}
