package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type sendTestEmailRequest struct {
	SequenceID *uuid.UUID `json:"step_id"`
	AccountID  uuid.UUID  `json:"account_id" binding:"required"`
	Recipient  string     `json:"recipient" binding:"required,email"`
}

// SendTestEmail sends a preview/test email for a campaign sequence
// POST /campaigns/:id/test-email
func (h *Handler) SendTestEmail(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID := middleware.GetUserID(c)

	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}

	var req sendTestEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	// Load campaign
	campaign, xerr := h.CampaignService.Get(c.Request.Context(), userID, campaignID.String())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// Load sequences
	sequences, err := h.TasksService.GetCampaignSequences(c.Request.Context(), campaignID)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if len(sequences) == 0 {
		errx.JSON(c, errx.New(errx.BadRequest, "campaign has no sequences"))
		return
	}

	// Select the right sequence
	var sequence *models.Sequence
	if req.SequenceID != nil {
		for i := range sequences {
			if sequences[i].ID == *req.SequenceID {
				sequence = &sequences[i]
				break
			}
		}
		if sequence == nil {
			errx.JSON(c, errx.New(errx.NotFound, "sequence not found"))
			return
		}
	} else {
		sequence = &sequences[0]
	}

	// Send the test email
	xerr = h.TasksService.SendTestEmail(c.Request.Context(), userID, req.AccountID, req.Recipient, campaign, sequence)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "test email sent",
		"recipient":  req.Recipient,
		"subject":    sequence.Subject,
		"account_id": req.AccountID.String(),
	})
}
