package grouph

import (
	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/group"
)

type Handler struct {
	name    string
	service group.GroupService
}

func New(r *gin.RouterGroup, service group.GroupService, name string) {
	h := &Handler{
		name:    name,
		service: service,
	}

	g := r.Group("/" + name)
	{
		g.POST("", h.Create)
		g.PATCH("/:gid", h.Update)
		g.PATCH("/:gid/move", h.Move)
		g.DELETE("/:gid", h.Delete)
	}
}
