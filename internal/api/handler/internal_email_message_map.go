package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
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
	if user, mailbox := mapScope(p.Map.UserID, p.Map.EmailID); !h.nodeMailbox(c, "email_message_map.arrival", http.StatusNotFound, user, mailbox) {
		return
	}
	repo, ok := h.EmailMessageMap.(repository.ArrivalAdmission)
	if !ok {
		logArrivalAdmissionFailure(c, p.Map, repository.ErrArrivalOutboxUnsupported)
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if err := repo.AdmitArrival(c.Request.Context(), repository.EmailMessageData(p.Map), p.Pending); err != nil {
		logArrivalAdmissionFailure(c, p.Map, err)
		if errors.Is(err, repository.ErrArrivalMailboxOwnershipLost) {
			if mailbox, parseErr := uuid.Parse(p.Map.EmailID); parseErr == nil && mailbox != uuid.Nil {
				c.Header("X-Warmbly-Arrival-Ownership-Lost", mailbox.String())
			}
		}
		// Do not expose message content or org encryption failures over the protocol.
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("X-Warmbly-Arrival-Durable", "1")
	c.Status(http.StatusNoContent)
}

func logArrivalAdmissionFailure(c *gin.Context, mapping emailMessageMapPayload, err error) {
	stage, state := repository.ArrivalAdmissionDiagnostic(err)
	event := log.Warn().Timestamp().Str("admission_stage", stage)
	if state != "" {
		event.Str("sqlstate", state)
	}
	for name, raw := range map[string]string{
		"user_id": mapping.UserID, "email_id": mapping.EmailID, "arrival_id": mapping.ID,
	} {
		if id, err := uuid.Parse(raw); err == nil && id != uuid.Nil {
			event.Str(name, id.String())
		}
	}
	if requestID := middleware.RequestID(c); requestID != "" {
		event.Str("request_id", requestID)
	}
	event.Msg("sync arrival admission deferred; durability not confirmed")
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

func (h *Handler) InternalLookupEmailMessageMaps(c *gin.Context) {
	var payload struct {
		UserID     string   `json:"user_id"`
		EmailID    string   `json:"email_id"`
		MessageIDs []string `json:"message_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)).Decode(&payload); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	userID, userErr := uuid.Parse(payload.UserID)
	emailID, emailErr := uuid.Parse(payload.EmailID)
	if userErr != nil || emailErr != nil || userID == uuid.Nil || emailID == uuid.Nil || len(payload.MessageIDs) > repository.EmailMessageMapBatchSize {
		c.Status(http.StatusBadRequest)
		return
	}
	if !h.nodeMailbox(c, "email_message_map.lookup", http.StatusNotFound, userID, emailID) {
		return
	}
	for _, id := range payload.MessageIDs {
		if id == "" {
			c.Status(http.StatusBadRequest)
			return
		}
	}
	mappings, err := repository.GetEmailMessageMaps(c.Request.Context(), h.EmailMessageMap, userID, emailID, payload.MessageIDs)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "message batch lookup failed"))
		return
	}
	items := make([]emailMessageMapPayload, 0, len(mappings))
	for _, mapping := range mappings {
		items = append(items, emailMessageMapPayload(*mapping))
	}
	c.JSON(http.StatusOK, gin.H{"mappings": items})
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
	if user, mailbox := mapScope(p.UserID, p.EmailID); !h.nodeMailbox(c, "email_message_map.put", http.StatusNotFound, user, mailbox) {
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
	if !h.nodeMailbox(c, "email_message_map.get", http.StatusNotFound, userID, emailID) {
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
	if !h.nodeMailbox(c, "email_message_map.delete", http.StatusNotFound, userID, emailID) {
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
