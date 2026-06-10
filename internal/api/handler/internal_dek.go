package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
)

// Internal endpoints used by workers to fetch/store encrypted DEKs without
// connecting to Postgres directly. Auth via middleware.InternalAuthMiddleware
// (static bearer token in INTERNAL_API_TOKEN env var, both sides).
//
// Wire format mirrors what encryptedkeys.HTTPStore expects:
//
//	GET    /api/v1/internal/dek/:orgID  -> 200 {"encrypted_data_key":"..."} | 404
//	PUT    /api/v1/internal/dek/:orgID  body: {"encrypted_data_key":"..."}
//	                                     -> 201 | 409 ErrAlreadyExists
//	DELETE /api/v1/internal/dek/:orgID  -> 204

type dekPayload struct {
	EncryptedDataKey string `json:"encrypted_data_key"`
}

func parseOrgID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("orgID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid orgID"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) InternalGetDEK(c *gin.Context) {
	id, ok := parseOrgID(c)
	if !ok {
		return
	}
	v, err := h.EncryptedKeys.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if v == "" {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, dekPayload{EncryptedDataKey: v})
}

func (h *Handler) InternalPutDEK(c *gin.Context) {
	id, ok := parseOrgID(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	var p dekPayload
	if err := json.Unmarshal(body, &p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decode body"})
		return
	}
	if p.EncryptedDataKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "encrypted_data_key required"})
		return
	}
	err = h.EncryptedKeys.Put(c.Request.Context(), id, p.EncryptedDataKey)
	switch {
	case err == nil:
		c.Status(http.StatusCreated)
	case errors.Is(err, encryptedkeys.ErrAlreadyExists):
		c.Status(http.StatusConflict)
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func (h *Handler) InternalDeleteDEK(c *gin.Context) {
	id, ok := parseOrgID(c)
	if !ok {
		return
	}
	if err := h.EncryptedKeys.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
