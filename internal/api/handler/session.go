package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
)

// currentSessionID returns the id of the session making the request, or the
// zero UUID if it can't be resolved (treated as "no current session").
func currentSessionID(c *gin.Context) uuid.UUID {
	if sess := middleware.GetSession(c); sess != nil {
		return sess.ID
	}
	return uuid.Nil
}

// SessionsList returns the authenticated user's active sessions for the
// account security page.
func (h *Handler) SessionsList(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	views, xerr := h.TokenService.ListSessions(c.Request.Context(), uid, currentSessionID(c))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, views)
}

// SessionRevoke ends one of the user's other sessions by id.
func (h *Handler) SessionRevoke(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	id, perr := uuid.Parse(c.Param("id"))
	if perr != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.TokenService.RevokeSessionByID(c.Request.Context(), uid, id, currentSessionID(c)); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

// SessionRevokeOthers ends every active session except the current one.
func (h *Handler) SessionRevokeOthers(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	if xerr := h.TokenService.RevokeOtherSessions(c.Request.Context(), uid, currentSessionID(c)); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}
