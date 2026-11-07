package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func errScopeField(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.Forbidden, middleware.MemberAccessRestricted, msg)
}

// restrictedCampaignUpdateAllowed keeps a restricted member's campaign edit off fields that reach other workspace resources.
func restrictedCampaignUpdateAllowed(c *gin.Context, data *models.UpdateCampaign) *errx.Error {
	if !middleware.IsScopeRestricted(c) {
		return nil
	}
	switch {
	case data.Status != nil:
		return errScopeField("Start or pause the campaign instead of setting its status.")
	case data.EmailTags != nil, data.SenderStrategy != nil:
		return errScopeField("The campaign's senders are set by your workspace's administrators.")
	case data.Folders != nil:
		return errScopeField("The campaign's folders are set by your workspace's administrators.")
	case data.TrackingDomain != nil:
		return errScopeField("The campaign's tracking domain is set by your workspace's administrators.")
	case data.CC != nil, data.BCC != nil:
		return errScopeField("The campaign's copied addresses are set by your workspace's administrators.")
	}
	return nil
}

// restrictedPoolAllowed holds a restricted member's campaign edits to campaigns that send only from mailboxes they were granted.
func (h *Handler) restrictedPoolAllowed(c *gin.Context, orgID uuid.UUID, id string) *errx.Error {
	if !middleware.IsScopeRestricted(c) {
		return nil
	}
	return h.campaignPoolAllowed(c, orgID, id)
}

// scopeStepActions are the step actions that touch nothing outside the campaign itself.
var scopeStepActions = map[string]bool{"wait": true, "end": true}

var errScopeStepAction = errScopeField("Steps that act on contacts, the CRM or automations are set by your workspace's administrators.")

// restrictedStepAllowed holds a restricted member's step edits to email, wait and end steps.
func restrictedStepAllowed(c *gin.Context, data *models.UpdateSequence) *errx.Error {
	if !middleware.IsScopeRestricted(c) {
		return nil
	}
	if data.Action != nil && !scopeStepActions[data.Action.Type] {
		return errScopeStepAction
	}
	if data.Kind != nil && *data.Kind == "action" && data.Action == nil {
		return errScopeStepAction
	}
	return nil
}
