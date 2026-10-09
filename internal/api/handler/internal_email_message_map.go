package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/repository"
)

func (h *Handler) InternalAdmitEmailArrival(c *gin.Context) {
	var p struct {
		Map     emailMessageMapPayload     `json:"map"`
		Pending *repository.PendingArrival `json:"pending"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)).Decode(&p); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	repo, ok := h.EmailMessageMap.(repository.ArrivalAdmission)
	if !ok {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if err := repo.AdmitArrival(c.Request.Context(), repository.EmailMessageData(p.Map), p.Pending); err != nil {
		// Do not expose message content or org encryption failures over the protocol.
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("X-Warmbly-Arrival-Durable", "1")
	c.Status(http.StatusNoContent)
}

// Internal email-message-map endpoints. Workers call these instead of touching
// Postgres directly (per CLAUDE.md), mirroring the DEK endpoints. Auth via
// middleware.NodeBrokerAuthMiddleware (the node token).
//
//	PUT    /api/v1/internal/email-message-map  body {user_id,email_id,message_id,id,thread_id} -> 204
//	GET    /api/v1/internal/email-message-map?user_id=&email_id=&message_id=
//	                                            -> 200 {...} | 404 (no mapping)
//	DELETE /api/v1/internal/email-message-map?user_id=&email_id=&message_id=&id= -> 204

type emailMessageMapPayload struct {
	UserID    string `json:"user_id"`
	EmailID   string `json:"email_id"`
	MessageID string `json:"message_id"`
	ID        string `json:"id"`
	ThreadID  string `json:"thread_id"`
}

func (h *Handler) InternalPutEmailMessageMap(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	var p emailMessageMapPayload
	if err := json.Unmarshal(body, &p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decode body"})
		return
	}
	if p.UserID == "" || p.EmailID == "" || p.MessageID == "" || p.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id, email_id, message_id, id required"})
		return
	}
	err = h.EmailMessageMap.Add(c.Request.Context(), repository.EmailMessageData{
		UserID:    p.UserID,
		EmailID:   p.EmailID,
		MessageID: p.MessageID,
		ID:        p.ID,
		ThreadID:  p.ThreadID,
	})
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) InternalGetEmailMessageMap(c *gin.Context) {
	userID, emailID, messageID, ok := parseEmailMessageMapKey(c)
	if !ok {
		return
	}
	data, err := h.EmailMessageMap.Get(c.Request.Context(), userID, emailID, messageID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if data == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, emailMessageMapPayload{
		UserID:    data.UserID,
		EmailID:   data.EmailID,
		MessageID: data.MessageID,
		ID:        data.ID,
		ThreadID:  data.ThreadID,
	})
}

func (h *Handler) InternalDeleteEmailMessageMap(c *gin.Context) {
	userID, emailID, messageID, ok := parseEmailMessageMapKey(c)
	if !ok {
		return
	}
	var id uuid.UUID
	if raw := c.Query("id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}
		id = parsed
	}
	if err := h.EmailMessageMap.Del(c.Request.Context(), userID, emailID, messageID, id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.Status(http.StatusNoContent)
}

func parseEmailMessageMapKey(c *gin.Context) (userID, emailID uuid.UUID, messageID string, ok bool) {
	var err error
	userID, err = uuid.Parse(c.Query("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return uuid.Nil, uuid.Nil, "", false
	}
	emailID, err = uuid.Parse(c.Query("email_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email_id"})
		return uuid.Nil, uuid.Nil, "", false
	}
	messageID = c.Query("message_id")
	if messageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message_id required"})
		return uuid.Nil, uuid.Nil, "", false
	}
	return userID, emailID, messageID, true
}
