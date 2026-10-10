package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

type completeOnboardingRequest struct {
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	ReferralSource string `json:"referral_source"`
	Role           string `json:"role"`
	TeamSize       string `json:"team_size"`
}

var validReferralSources = map[string]bool{
	"reddit":   true,
	"x":        true,
	"facebook": true,
	"google":   true,
	"other":    true,
}

// Persona + team-size answers from the onboarding questionnaire. Both optional:
// when provided they must be one of these, otherwise they're stored as NULL.
var validRoles = map[string]bool{
	"founder":   true,
	"sales":     true,
	"marketing": true,
	"agency":    true,
	"recruiter": true,
	"other":     true,
}

var validTeamSizes = map[string]bool{
	"just_me": true,
	"2-10":    true,
	"11-50":   true,
	"51-200":  true,
	"200+":    true,
}

func (h *Handler) CompleteOnboarding(c *gin.Context) {
	var req completeOnboardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}

	first, last, xerr := validatePersonName(req.FirstName, req.LastName)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	req.FirstName, req.LastName = first, last

	if !validReferralSources[req.ReferralSource] {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid referral source."))
		return
	}

	if req.Role != "" && !validRoles[req.Role] {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid role."))
		return
	}

	if req.TeamSize != "" && !validTeamSizes[req.TeamSize] {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid team size."))
		return
	}

	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	if xerr := h.UserService.CompleteOnboarding(c.Request.Context(), uid, req.FirstName, req.LastName, req.ReferralSource, req.Role, req.TeamSize); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

type updateProfileRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// UpdateUserProfile persists editable profile fields (first/last name) from the
// profile settings page. Unlike onboarding, it carries no questionnaire answers
// and can be called any time the user renames themselves.
func (h *Handler) UpdateUserProfile(c *gin.Context) {
	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}

	first, last, xerr := validatePersonName(req.FirstName, req.LastName)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	req.FirstName, req.LastName = first, last

	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	if xerr := h.UserService.UpdateProfile(c.Request.Context(), uid, req.FirstName, req.LastName); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

type updateSendPreferencesRequest struct {
	UndoSendSeconds int `json:"undo_send_seconds"`
}

// UpdateSendPreferences persists the user's undo-send window: how long an
// instant send is held (and still cancellable) before it actually leaves.
// PUT /me/send-preferences
func (h *Handler) UpdateSendPreferences(c *gin.Context) {
	var req updateSendPreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	if req.UndoSendSeconds < config.UndoSendSecondsMin || req.UndoSendSeconds > config.UndoSendSecondsMax {
		errx.Handle(c, errx.New(errx.BadRequest, fmt.Sprintf(
			"undo_send_seconds must be between %d and %d",
			config.UndoSendSecondsMin, config.UndoSendSecondsMax,
		)))
		return
	}

	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	if xerr := h.UserService.UpdateUndoSendSeconds(c.Request.Context(), uid, req.UndoSendSeconds); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"undo_send_seconds": req.UndoSendSeconds})
}

// CompleteProductTour records that the caller finished or skipped the product
// tour. Repeating it is harmless: the first time is kept.
func (h *Handler) CompleteProductTour(c *gin.Context) {
	uid, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	at, xerr := h.UserService.CompleteProductTour(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"product_tour_completed_at": at})
}

// validatePersonName applies the display-name rules to a first and last name,
// returning their stored forms.
func validatePersonName(first, last string) (string, string, *errx.Error) {
	first, xerr := displayname.Validate("First name", first, displayname.Person, false)
	if xerr != nil {
		return "", "", xerr
	}
	last, xerr = displayname.Validate("Last name", last, displayname.Person, false)
	if xerr != nil {
		return "", "", xerr
	}
	return first, last, nil
}
