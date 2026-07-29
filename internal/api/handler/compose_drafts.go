// Compose draft endpoints: autosaved, per-user working copies of unsent
// emails from the compose window. Personal data (user-scoped within the org),
// so there is no audit fanout; the client generates the draft id and PUTs the
// whole draft on a debounce, which makes autosave naturally idempotent.
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	maxDraftBody    = 100_000
	maxDraftSubject = 1_000
	maxDraftRcpts   = 100
)

// ListComposeDrafts — GET /unibox/drafts
func (h *Handler) ListComposeDrafts(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	drafts, xerr := h.ComposeService.ListDrafts(c.Request.Context(), userID, *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": drafts})
}

type composeDraftUpsertRequest struct {
	EmailAccountID string   `json:"email_account_id"`
	To             []string `json:"to"`
	CC             []string `json:"cc"`
	BCC            []string `json:"bcc"`
	Subject        string   `json:"subject"`
	Body           string   `json:"body"`
}

// UpsertComposeDraft — PUT /unibox/drafts/:id
func (h *Handler) UpsertComposeDraft(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var req composeDraftUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	if len(req.Body) > maxDraftBody || len(req.Subject) > maxDraftSubject ||
		len(req.To) > maxDraftRcpts || len(req.CC) > maxDraftRcpts || len(req.BCC) > maxDraftRcpts {
		errx.Handle(c, errx.New(errx.BadRequest, "draft is too large"))
		return
	}

	d := &repository.ComposeDraft{
		ID:      id,
		To:      emptyIfNil(req.To),
		CC:      emptyIfNil(req.CC),
		BCC:     emptyIfNil(req.BCC),
		Subject: req.Subject,
		Body:    req.Body,
	}
	if v := strings.TrimSpace(req.EmailAccountID); v != "" && v != "auto" {
		if accountID, perr := uuid.Parse(v); perr == nil {
			d.EmailAccountID = &accountID
		}
	}

	if xerr := h.ComposeService.UpsertDraft(c.Request.Context(), userID, *orgID, d); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// DeleteComposeDraft — DELETE /unibox/drafts/:id
func (h *Handler) DeleteComposeDraft(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	if xerr := h.ComposeService.DeleteDraft(c.Request.Context(), userID, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
