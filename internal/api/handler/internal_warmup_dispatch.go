package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (h *Handler) InternalWarmupDispatch(c *gin.Context) {
	if h.WarmupDispatch == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	var req struct {
		TaskID    uuid.UUID               `json:"task_id"`
		MailboxID uuid.UUID               `json:"mailbox_id"`
		WorkerID  uuid.UUID               `json:"worker_id"`
		Nonce     uuid.UUID               `json:"nonce"`
		Start     bool                    `json:"start"`
		Result    *models.SendEmailResult `json:"result,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TaskID == uuid.Nil || req.MailboxID == uuid.Nil || req.WorkerID == uuid.Nil {
		c.Status(http.StatusBadRequest)
		return
	}
	ctx := c.Request.Context()
	if admission, ok := h.WarmupDispatch.(repository.OutboundAdmissionRepository); ok {
		state, err := admission.InspectOutbound(ctx, req.TaskID, req.MailboxID, req.WorkerID)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if state.State != "legacy" {
			if req.Result != nil {
				if req.Start || req.Result.TaskID != req.TaskID {
					c.Status(http.StatusBadRequest)
					return
				}
				if err = admission.FinishOutbound(ctx, req.TaskID, req.MailboxID, req.WorkerID, *req.Result); err != nil {
					c.Status(http.StatusConflict)
					return
				}
				c.Status(http.StatusNoContent)
				return
			}
			if req.Start && state.State == "authorized" {
				if req.Nonce == uuid.Nil {
					c.Status(http.StatusBadRequest)
					return
				}
				validator, ok := h.TasksService.(interface {
					ValidateOutboundExecution(context.Context, uuid.UUID) error
				})
				if !ok {
					c.Status(http.StatusServiceUnavailable)
					return
				}
				if err = validator.ValidateOutboundExecution(ctx, req.TaskID); err != nil {
					if err = admission.CancelOutbound(ctx, req.TaskID, req.MailboxID, req.WorkerID, req.Nonce); err != nil {
						c.Status(http.StatusServiceUnavailable)
						return
					}
					c.JSON(http.StatusOK, gin.H{"state": "denied"})
					return
				}
				state, err = admission.BeginOutbound(ctx, req.TaskID, req.MailboxID, req.WorkerID, req.Nonce)
				if err != nil {
					c.Status(http.StatusServiceUnavailable)
					return
				}
			}
			c.JSON(http.StatusOK, state)
			return
		}
	}
	if req.Result != nil {
		if req.Start || req.Result.TaskID != req.TaskID {
			c.Status(http.StatusBadRequest)
			return
		}
		if err := h.WarmupDispatch.FinishWarmupDispatch(ctx, req.TaskID, req.MailboxID, req.WorkerID, *req.Result); err != nil {
			c.Status(http.StatusConflict)
			return
		}
		c.Status(http.StatusNoContent)
		return
	}
	if req.Start {
		if req.Nonce == uuid.Nil {
			c.Status(http.StatusBadRequest)
			return
		}
		validator, ok := h.TasksService.(interface {
			ValidateWarmupExecution(context.Context, uuid.UUID) error
		})
		if !ok {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if err := validator.ValidateWarmupExecution(ctx, req.TaskID); err != nil {
			if err := h.WarmupDispatch.DeferWarmupDispatch(ctx, req.TaskID, req.MailboxID, req.WorkerID, req.Nonce, time.Now().Add(5*time.Minute)); err != nil {
				c.Status(http.StatusServiceUnavailable)
				return
			}
			c.JSON(http.StatusOK, gin.H{"state": "deferred"})
			return
		}
		state, err := h.WarmupDispatch.BeginWarmupDispatch(ctx, req.TaskID, req.MailboxID, req.WorkerID, req.Nonce)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.JSON(http.StatusOK, state)
		return
	}
	state, err := h.WarmupDispatch.InspectWarmupDispatch(ctx, req.TaskID, req.MailboxID, req.WorkerID)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusOK, state)
}
