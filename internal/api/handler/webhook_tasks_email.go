package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	protob "google.golang.org/protobuf/proto"
)

func (h *Handler) HandleEmailTask(c *gin.Context) {
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

	// Dispatch by the task row's type: every enqueue targets this one webhook
	// URL, so campaign and user-email callbacks land here too.
	if err := h.TasksService.HandleTask(&taskPayload); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
