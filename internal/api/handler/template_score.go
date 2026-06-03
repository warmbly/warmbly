package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

type scoreTemplateRequest struct {
	Subject   string `json:"subject"`
	BodyHTML  string `json:"body_html"`
	BodyPlain string `json:"body_plain"`
}

// ScoreTemplateContent returns an advisory deliverability content score for a
// campaign template (subject + body) before it is sent. Advisory only — it
// never blocks sending — so the user gets content feedback on the mail that
// actually reaches prospects, which warmup-only linting never covered.
func (h *Handler) ScoreTemplateContent(c *gin.Context) {
	var req scoreTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	res := warmlint.Score(req.Subject, req.BodyHTML, req.BodyPlain)
	c.JSON(http.StatusOK, res)
}
