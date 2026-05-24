package handler

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/socket"
	"github.com/warmbly/warmbly/internal/errx"
)

func (h *Handler) GenerateWebsocket(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	token, xerr := h.SocketService.GenerateWebsocketToken(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	wsURL := token
	if h.WebsocketURI != "" {
		u, err := url.Parse(h.WebsocketURI)
		if err != nil {
			errx.Handle(c, errx.InternalError())
			return
		}

		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
		wsURL = u.String()
	}

	c.JSON(http.StatusOK, gin.H{
		"url":        wsURL,
		"expires_in": socket.SocketTTL.Seconds(),
	})
}
