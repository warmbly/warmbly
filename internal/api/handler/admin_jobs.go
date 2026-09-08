// Admin endpoints for the scheduled jobs registry: every background loop the
// backend and consumer run, with its last run, and a "run now" request the
// owning loop picks up on its next poll.

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const scheduledJobEntity models.AuditEntityType = "scheduled_job"

// AdminListJobs lists every registered background loop.
func (h *Handler) AdminListJobs(c *gin.Context) {
	if h.JobRuns == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "job registry is not available on this instance"))
		return
	}
	jobs, err := h.JobRuns.List(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if jobs == nil {
		jobs = []models.ScheduledJobRun{}
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// AdminRunJob asks the loop that owns the job to run at its next poll.
func (h *Handler) AdminRunJob(c *gin.Context) {
	if h.JobRuns == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "job registry is not available on this instance"))
		return
	}
	name := c.Param("name")
	if name == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "job name is required"))
		return
	}
	found, err := h.JobRuns.RequestRun(c.Request.Context(), name)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if !found {
		errx.JSON(c, errx.New(errx.NotFound, "job not found"))
		return
	}
	h.audit(c, models.AuditActionStart, scheduledJobEntity, nil, map[string]string{"job": name})
	c.JSON(http.StatusOK, gin.H{"requested": true})
}
