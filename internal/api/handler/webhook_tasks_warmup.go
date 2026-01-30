package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	protob "google.golang.org/protobuf/proto"
)

func (h *Handler) HandleWarmupTask(c *gin.Context) {
	rawData, err := c.GetRawData()
	if err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	var taskPayload proto.ProcessTask

	if err := protob.Unmarshal(rawData, &taskPayload); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if err := h.TasksService.HandleEmailTask(&taskPayload); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
