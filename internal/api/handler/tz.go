package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetTimezones(c *gin.Context) {
	c.JSON(http.StatusOK, h.TzService.Timezones())
}
