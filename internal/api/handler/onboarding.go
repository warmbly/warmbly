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
}

var validReferralSources = map[string]bool{
	"reddit":   true,
	"x":        true,
	"facebook": true,
	"google":   true,
	"other":    true,
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

	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	if xerr := h.UserService.CompleteOnboarding(c.Request.Context(), uid, req.FirstName, req.LastName, req.ReferralSource); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}
