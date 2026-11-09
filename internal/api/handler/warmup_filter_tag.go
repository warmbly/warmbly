package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

type warmupFilterTagResponse struct {
	Tag *string `json:"tag"`
}

type setWarmupFilterTagRequest struct {
	// Tag is the code to use; empty issues a new random one.
	Tag string `json:"tag"`
}

func (h *Handler) warmupFilterTagTarget(c *gin.Context) (orgID, accountID uuid.UUID, ok bool) {
	org := middleware.GetOrganizationID(c)
	if org == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return uuid.Nil, uuid.Nil, false
	}
	if h.WarmupFilterTags == nil {
		errx.Handle(c, errx.InternalError())
		return uuid.Nil, uuid.Nil, false
	}
	return *org, id, true
}

// GetWarmupFilterTag returns the code appended to warmup mail this mailbox receives, null when off.
func (h *Handler) GetWarmupFilterTag(c *gin.Context) {
	orgID, accountID, ok := h.warmupFilterTagTarget(c)
	if !ok {
		return
	}
	tag, found, err := h.WarmupFilterTags.Get(c.Request.Context(), orgID, accountID)
	if err != nil {
		log.Error().Err(err).Str("email_account_id", accountID.String()).Msg("warmup filter tag read failed")
		errx.Handle(c, errx.InternalError())
		return
	}
	if !found {
		errx.Handle(c, errx.ErrNotFound)
		return
	}
	c.JSON(http.StatusOK, warmupFilterTagResponse{Tag: tag})
}

// SetWarmupFilterTag turns the tag on with a chosen or new random code.
func (h *Handler) SetWarmupFilterTag(c *gin.Context) {
	orgID, accountID, ok := h.warmupFilterTagTarget(c)
	if !ok {
		return
	}
	var req setWarmupFilterTagRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "invalid request body"))
			return
		}
	}
	tag := models.NewWarmupFilterTag()
	// A random code that happens to spell a self-describing word is drawn again.
	for generation.ValidateWarmupWording(tag) != nil {
		tag = models.NewWarmupFilterTag()
	}
	if req.Tag != "" {
		cleaned, err := models.CleanWarmupFilterTag(req.Tag)
		if err == nil && generation.ValidateWarmupWording(cleaned) != nil {
			err = errInvalidFilterTagWording
		}
		if err != nil {
			errx.Handle(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_filter_tag", err.Error()))
			return
		}
		tag = cleaned
	}
	h.storeWarmupFilterTag(c, orgID, accountID, &tag)
}

// ClearWarmupFilterTag turns the tag off; warmup mail carries no visible marker again.
func (h *Handler) ClearWarmupFilterTag(c *gin.Context) {
	orgID, accountID, ok := h.warmupFilterTagTarget(c)
	if !ok {
		return
	}
	h.storeWarmupFilterTag(c, orgID, accountID, nil)
}

func (h *Handler) storeWarmupFilterTag(c *gin.Context, orgID, accountID uuid.UUID, tag *string) {
	found, err := h.WarmupFilterTags.Set(c.Request.Context(), orgID, accountID, tag)
	if err != nil {
		log.Error().Err(err).Str("email_account_id", accountID.String()).Msg("warmup filter tag write failed")
		errx.Handle(c, errx.InternalError())
		return
	}
	if !found {
		errx.Handle(c, errx.ErrNotFound)
		return
	}
	state := "off"
	if tag != nil {
		state = "on"
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, map[string]string{"warmup_filter_tag": state}, nil)
	c.JSON(http.StatusOK, warmupFilterTagResponse{Tag: tag})
}

var errInvalidFilterTagWording = errors.New("a filter tag cannot describe the mail as warmup, automated or a test; use a code instead")
