package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Admin endpoints for /admin/settings/backends — surface the storage_backends
// table to the admin UI. Today most backends are read-only (env-var driven for
// KMS / EncryptedKeys / EventBus); the Activate path exists so once
// admin-mutable backends (Blob primarily) are wired into runtime reload, the
// UI can flip the active row without code changes.

func (h *Handler) AdminListStorageBackends(c *gin.Context) {
	if kind := c.Query("kind"); kind != "" {
		rows, err := h.StorageBackendRepo.ListByKind(c.Request.Context(), kind)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"backends": rows})
		return
	}
	rows, err := h.StorageBackendRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backends": rows})
}

func (h *Handler) AdminGetActiveStorageBackend(c *gin.Context) {
	kind := c.Param("kind")
	row, err := h.StorageBackendRepo.GetActive(c.Request.Context(), kind)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active backend for kind"})
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *Handler) AdminActivateStorageBackend(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.StorageBackendRepo.SetActive(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "activated"})
}
