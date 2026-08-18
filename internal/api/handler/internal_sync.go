package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// InternalSyncOwnConversation answers the worker's priority-lane question:
// does any of the given RFC message ids, or the provider thread id, belong to
// a message this mailbox sent or already holds? Workers call it once per new
// message so a reply to outreach is admitted ahead of fair-use throttling.
//
//	GET /api/v1/internal/sync/own-conversation?user_id=&email_id=&message_ids=a,b&thread_id=
//	    -> 200 {"own": bool}
func (h *Handler) InternalSyncOwnConversation(c *gin.Context) {
	if h.EmailSyncState == nil {
		c.JSON(http.StatusOK, gin.H{"own": false})
		return
	}
	userID, err := uuid.Parse(c.Query("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}
	emailID, err := uuid.Parse(c.Query("email_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email_id"})
		return
	}
	var ids []string
	for _, raw := range strings.Split(c.Query("message_ids"), ",") {
		if id := strings.TrimSpace(raw); id != "" {
			ids = append(ids, id)
		}
	}
	own, err := h.EmailSyncState.IsOwnConversation(c.Request.Context(), userID, emailID, ids, c.Query("thread_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"own": own})
}
