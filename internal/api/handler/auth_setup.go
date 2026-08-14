package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
)

type setupClaimRequest struct {
	Token     string `json:"token"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// SetupClaim exchanges the one-time link printed at first boot for the owner
// account, and signs them straight in.
//
// Public by necessity: there is no account to authenticate as yet. The token is
// the protection, and it is consumed atomically, refused once any user exists,
// and covered by the same per-IP limiter as the rest of the auth group.
func (h *Handler) SetupClaim(c *gin.Context) {
	if h.BootstrapService == nil {
		errx.Handle(c, errx.New(errx.NotFound, "Setup is not available on this deployment."))
		return
	}

	var req setupClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	u, xerr := h.BootstrapService.Claim(
		c.Request.Context(),
		req.Token,
		req.Email,
		req.Password,
		req.FirstName,
		req.LastName,
	)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	// Hand back a session rather than bouncing them to the login screen. They
	// just proved ownership of the instance; asking them to type the password
	// again immediately is friction with no security value.
	session, terr := h.TokenService.GenerateSession(
		c.Request.Context(),
		u.ID,
		u.Email,
		c.ClientIP(),
		c.Request.UserAgent(),
		token.AuthProviderEmail,
	)
	if terr != nil {
		errx.Handle(c, terr)
		return
	}

	c.JSON(http.StatusOK, session)
}
