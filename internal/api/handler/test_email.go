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
	// ContactID renders the copy for a real contact of the organization
	// instead of the placeholder one.
	ContactID *uuid.UUID `json:"contact_id"`
}

// SendTestEmail sends a preview/test email for a campaign sequence
// POST /campaigns/:id/test-email
func (h *Handler) SendTestEmail(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

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
	campaign, xerr := h.CampaignService.Get(c.Request.Context(), orgID.String(), campaignID.String())
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
	idx := 0
	if req.SequenceID != nil {
		idx = -1
		for i := range sequences {
			if sequences[i].ID == *req.SequenceID {
				idx = i
				break
			}
		}
		if idx < 0 {
			errx.JSON(c, errx.New(errx.NotFound, "sequence not found"))
			return
		}
	}
	// A step that replies in the contact's thread has no subject of its own,
	// so the test would arrive blank without resolving the conversation's.
	step := sequences[idx]
	step.Subject = models.StepSubject(sequences, idx)
	sequence := &step

	if xerr := mailboxAllowed(c, req.AccountID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	var contact *models.Contact
	if req.ContactID != nil {
		// Rendering a real contact reads its fields back, so it takes the
		// contacts read permission on top of the route's campaigns one.
		if xerr := h.hasAccess(c, models.PermViewContacts, models.APIPermReadContacts); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		found, cxerr := h.orgContact(c.Request.Context(), *orgID, *req.ContactID)
		if cxerr != nil {
			errx.JSON(c, cxerr)
			return
		}
		contact = found
	}

	// Send the test email
	xerr = h.TasksService.SendTestEmail(c.Request.Context(), *orgID, req.AccountID, req.Recipient, campaign, sequence, contact)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	resp := gin.H{
		"message":    "test email sent",
		"recipient":  req.Recipient,
		"subject":    sequence.Subject,
		"account_id": req.AccountID.String(),
		"step_id":    sequence.ID.String(),
	}
	if contact != nil {
		resp["contact_id"] = contact.ID.String()
	}
	c.JSON(http.StatusOK, resp)
}
