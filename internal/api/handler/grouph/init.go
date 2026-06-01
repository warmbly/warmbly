package grouph

import (
	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/group"
)

type Handler struct {
	name    string
	service group.GroupService
	audit   audit.AuditService
}

func New(r *gin.RouterGroup, service group.GroupService, auditService audit.AuditService, name string, middleware ...gin.HandlerFunc) {
	h := &Handler{
		name:    name,
		service: service,
		audit:   auditService,
	}

	g := r.Group("/" + name)
	if len(middleware) > 0 {
		g.Use(middleware...)
	}
	{
		g.POST("", h.Create)
		g.PATCH("/:gid", h.Update)
		g.PATCH("/:gid/move", h.Move)
		g.DELETE("/:gid", h.Delete)
	}
}
