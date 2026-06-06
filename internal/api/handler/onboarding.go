package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
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
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if req.FirstName == "" || req.LastName == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "First name and last name are required."))
		return
	}

	if len(req.FirstName) > 50 || len(req.LastName) > 50 {
		errx.Handle(c, errx.New(errx.BadRequest, "Name must be 50 characters or less."))
		return
	}

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
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if req.FirstName == "" || req.LastName == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "First name and last name are required."))
		return
	}
	if len(req.FirstName) > 50 || len(req.LastName) > 50 {
		errx.Handle(c, errx.New(errx.BadRequest, "Name must be 50 characters or less."))
		return
	}

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
