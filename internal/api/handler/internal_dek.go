package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
)

// Internal endpoints used by workers to fetch/store encrypted DEKs without
// connecting to Postgres directly. Auth via middleware.NodeBrokerAuthMiddleware
// (NODE_BROKER_TOKEN, falling back to INTERNAL_API_TOKEN).
//
// Wire format mirrors what encryptedkeys.HTTPStore expects:
//
//	GET    /api/v1/internal/dek/:orgID  -> 200 {"encrypted_data_key":"..."} | 404
//	PUT    /api/v1/internal/dek/:orgID  body: {"encrypted_data_key":"..."}
//	                                     -> 201 | 409 ErrAlreadyExists
//	DELETE /api/v1/internal/dek/:orgID  -> 204
//	POST   /api/v1/internal/dek/decrypt body: {"encrypted_data_key":"..."}
//	                                     -> 200 {"data_key":"<base64>"}

type dekPayload struct {
	EncryptedDataKey string `json:"encrypted_data_key"`
}

type dekDecryptResponse struct {
	DataKey string `json:"data_key"`
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
	if !h.nodeOrganization(c, "dek.get", id) {
		return
	}
	v, err := h.EncryptedKeys.Get(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
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
	if !h.nodeOrganization(c, "dek.put", id) {
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
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
	}
}

// InternalDecryptDEK opens a sealed data key for a node, so the node needs no
// credential for the KMS behind it.
//
// Without this a worker running against AWS KMS needs its own IAM credential
// good for kms:Decrypt, which means a long-lived access key on every machine
// in the fleet. This endpoint replaces that with the internal API token the
// node already holds, which is scoped to this instance and revocable from it.
//
// It takes ciphertext and returns plaintext, with no organization id anywhere
// in the exchange: a caller can only open a key it was already given, so this
// grants nothing beyond what holding the sealed key and the token already did.
// A worker that presents its own credential opens only its assigned organizations' keys.
func (h *Handler) InternalDecryptDEK(c *gin.Context) {
	if h.KMS == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "no kms provider configured"})
		return
	}
	var p dekPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decode body"})
		return
	}
	if p.EncryptedDataKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "encrypted_data_key required"})
		return
	}
	if !h.nodeInScope(c, "dek.decrypt", http.StatusNotFound, func(ctx context.Context, node uuid.UUID) (bool, error) {
		return h.NodeScope.DataKey(ctx, node, p.EncryptedDataKey)
	}) {
		logBrokerAccess(c, "dek.decrypt", "", false, "data key outside the node's assignment")
		return
	}
	key, err := h.KMS.GetDecryptedKey(c.Request.Context(), p.EncryptedDataKey)
	if err != nil {
		// The reason is not echoed: this answers an unauthenticated-by-org
		// caller, and KMS errors distinguish "not a key of ours" from "malformed",
		// which is exactly what a prober wants to learn. It is recorded, though:
		// a run of failures here is the signal that someone holding the token is
		// trying keys.
		logBrokerAccess(c, "dek.decrypt", "", false, "kms refused the ciphertext")
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not decrypt data key"})
		return
	}
	logBrokerAccess(c, "dek.decrypt", "", true, "")
	c.JSON(http.StatusOK, dekDecryptResponse{DataKey: base64.StdEncoding.EncodeToString(key)})
}
