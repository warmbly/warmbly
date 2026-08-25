package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

type scoreTemplateRequest struct {
	Subject         string `json:"subject"`
	BodyHTML        string `json:"body_html"`
	BodyPlain       string `json:"body_plain"`
	AttachmentCount int    `json:"attachment_count"`
	ImageCount      int    `json:"image_count"`
}

// ScoreTemplateContent returns the content score used by the send guardrail.
func (h *Handler) ScoreTemplateContent(c *gin.Context) {
	var req scoreTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	res := warmlint.ScoreWithOptions(req.Subject, req.BodyHTML, req.BodyPlain, warmlint.ScoreOptions{
		AttachmentCount: req.AttachmentCount,
		ImageCount:      req.ImageCount,
	})
	c.JSON(http.StatusOK, res)
}
