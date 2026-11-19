package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
)

// The admin panel's GraphKit connection. Secrets go in and never come out; the environment wins over all of it.

func (h *Handler) storeConnectionReady(c *gin.Context) bool {
	if h.StoreService == nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.NotFound, store.CodeDisabled, store.NotConfiguredMessage))
		return false
	}
	return true
}

// AdminGetStoreConnection is GET /admin/store/connection.
func (h *Handler) AdminGetStoreConnection(c *gin.Context) {
	if !h.storeConnectionReady(c) {
		return
	}
	out, err := h.StoreService.ConnectionView(c.Request.Context())
	if err != nil {
		storeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

func bindConnection(c *gin.Context) (store.ConnectionInput, bool) {
	var in store.ConnectionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return in, false
	}
	return in, true
}

// AdminPutStoreConnection is PUT /admin/store/connection: tests the key, stores it sealed, and turns the store on.
func (h *Handler) AdminPutStoreConnection(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if !h.storeConnectionReady(c) {
		return
	}
	in, ok := bindConnection(c)
	if !ok {
		return
	}
	out, err := h.StoreService.SaveConnection(c.Request.Context(), in, *adminID)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditStoreConnection(c, "store_connection_saved", map[string]any{
		"api_url": out.APIURL, "mode": out.Mode, "api_key_changed": in.APIKey != "", "webhook_secret_changed": in.WebhookSecret != "",
	})
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

// AdminTestStoreConnection is POST /admin/store/connection/test: asks GraphKit with what was typed, saving nothing.
func (h *Handler) AdminTestStoreConnection(c *gin.Context) {
	if !h.storeConnectionReady(c) {
		return
	}
	in, ok := bindConnection(c)
	if !ok {
		return
	}
	out, err := h.StoreService.TestConnection(c.Request.Context(), in)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// AdminDeleteStoreConnection is DELETE /admin/store/connection: deletes the stored key and turns the store off.
func (h *Handler) AdminDeleteStoreConnection(c *gin.Context) {
	if !h.storeConnectionReady(c) {
		return
	}
	out, err := h.StoreService.ClearConnection(c.Request.Context())
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditStoreConnection(c, "store_connection_removed", nil)
	c.JSON(http.StatusOK, out)
}

// auditStoreConnection records a connection change; details never carry a secret.
func (h *Handler) auditStoreConnection(c *gin.Context, action string, details map[string]any) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil || h.AdminService == nil {
		return
	}
	h.AdminService.LogAdminAction(c.Request.Context(), *adminID, action, "instance", nil, details, c.ClientIP(), c.Request.UserAgent())
}
