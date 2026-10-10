package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/app/nodelogs"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
)

func (h *Handler) logNodeExists(c *gin.Context, id uuid.UUID) bool {
	if h.FleetNodeRepo == nil {
		errx.JSON(c, errx.ErrServiceDown)
		return false
	}
	_, err := h.FleetNodeRepo.Get(c.Request.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		errx.JSON(c, errx.ErrNotFound)
		return false
	}
	if err != nil {
		errx.JSON(c, errx.ErrServiceDown)
		return false
	}
	return true
}

func (h *Handler) InternalNodeLogs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.NodeLogs == nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	secret := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if err := h.NodeLogs.Authenticate(c.Request.Context(), id, secret); err != nil {
		if errors.Is(err, nodelogs.ErrRateLimited) {
			c.Header("Retry-After", "60")
			errx.JSON(c, errx.New(errx.TooManyRequests, "node evidence rate exceeded"))
			return
		}
		if errors.Is(err, nodelogs.ErrUnauthorized) {
			errx.JSON(c, errx.ErrUnauthorized)
		} else {
			errx.JSON(c, errx.ErrServiceDown)
		}
		return
	}
	if !h.logNodeExists(c, id) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	var batch nodeevidence.Batch
	if err := c.ShouldBindJSON(&batch); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid evidence batch"))
		return
	}
	if err := h.NodeLogs.Ingest(c.Request.Context(), id, secret, batch); err != nil {
		if errors.Is(err, nodelogs.ErrUnauthorized) {
			errx.JSON(c, errx.ErrUnauthorized)
		} else if errors.Is(err, nodelogs.ErrInvalid) {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid evidence batch"))
		} else {
			errx.JSON(c, errx.ErrServiceDown)
		}
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) AdminNodeLogs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.NodeLogs == nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	id, ok := h.parseID(c)
	if !ok || !h.logNodeExists(c, id) {
		return
	}
	f := nodelogs.Filter{Level: c.Query("level"), Limit: nodelogs.MaxRead}
	if f.Level != "" && f.Level != "info" && f.Level != "warn" && f.Level != "error" {
		errx.JSON(c, errx.New(errx.BadRequest, "level must be info, warn or error"))
		return
	}
	for key, dest := range map[string]*time.Time{"after": &f.After, "before": &f.Before} {
		if value := c.Query(key); value != "" {
			at, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				errx.JSON(c, errx.New(errx.BadRequest, "after and before must be RFC3339 timestamps"))
				return
			}
			*dest = at
		}
	}
	if !f.After.IsZero() && !f.Before.IsZero() && f.After.After(f.Before) {
		errx.JSON(c, errx.New(errx.BadRequest, "after must not follow before"))
		return
	}
	if value := c.Query("mailbox_id"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "mailbox_id must be a uuid"))
			return
		}
		f.MailboxID = id
	}
	if value := c.Query("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > nodelogs.MaxRead {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		f.Limit = n
	}
	out, err := h.NodeLogs.History(c.Request.Context(), id, f)
	if err != nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	c.JSON(http.StatusOK, out)
}
