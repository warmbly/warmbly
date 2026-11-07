package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
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
	if !h.nodeMailbox(c, "sync.own_conversation", http.StatusNotFound, userID, emailID) {
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
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"own": own})
}

// InternalSyncFolderMessages answers the worker's expunge reconciliation for
// one IMAP folder: what does the platform still hold here, keyed by UID? The
// worker compares it with the folder's current UID set and removes the rows
// the server no longer reports. uid_validity scopes the answer to the current
// generation, so a UIDVALIDITY change is never read as a folder-wide expunge.
//
//	GET /api/v1/internal/sync/folder-messages?user_id=&email_id=&folder_path=&uid_validity=
//	    -> 200 {"messages":[{"uid":1,"id":"...","message_id":"..."}]}
func (h *Handler) InternalSyncFolderMessages(c *gin.Context) {
	if h.EmailSyncState == nil {
		c.JSON(http.StatusOK, gin.H{"messages": []repository.StoredFolderMessage{}})
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
	if !h.nodeMailbox(c, "sync.folder_messages", http.StatusNotFound, userID, emailID) {
		return
	}
	folderPath := c.Query("folder_path")
	if folderPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "folder_path required"})
		return
	}
	uidValidity, err := strconv.ParseUint(c.Query("uid_validity"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid uid_validity"})
		return
	}
	messages, err := h.EmailSyncState.ListFolderMessages(c.Request.Context(), userID, emailID, folderPath, uint32(uidValidity))
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

// maxProviderFolderMessages bounds one provider-folder listing.
const maxProviderFolderMessages = 1000

func (h *Handler) InternalSyncProviderMessages(c *gin.Context) {
	userID, err := uuid.Parse(c.Query("user_id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	emailID, err := uuid.Parse(c.Query("email_id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	if !h.nodeMailbox(c, "sync.provider_messages", http.StatusNotFound, userID, emailID) {
		return
	}
	var after *uuid.UUID
	if raw := c.Query("after"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			errx.JSON(c, errx.ErrUuid)
			return
		}
		after = &id
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil || limit < 1 || limit > maxProviderFolderMessages {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid limit"))
		return
	}
	if h.EmailSyncState == nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	messages, err := h.EmailSyncState.ListProviderMessages(c.Request.Context(), userID, emailID, after, limit)
	if err != nil {
		errs.CaptureException(err)
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

// InternalSyncProviderFolderMessages answers the Gmail folder reconciliation:
// which rows does the platform still believe the provider has in these
// folders? The worker checks each against Gmail's labels and reports the ones
// that moved.
//
//	GET /api/v1/internal/sync/provider-folder-messages?user_id=&email_id=&folders=inbox,archive&limit=
//	    -> 200 {"messages":[{"id":"...","provider_id":"...","provider_folder":"inbox","internal_date":"..."}]}
func (h *Handler) InternalSyncProviderFolderMessages(c *gin.Context) {
	if h.EmailSyncState == nil {
		c.JSON(http.StatusOK, gin.H{"messages": []repository.ProviderFolderMessage{}})
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
	if !h.nodeMailbox(c, "sync.provider_folder_messages", http.StatusNotFound, userID, emailID) {
		return
	}
	var folders []string
	for _, raw := range strings.Split(c.Query("folders"), ",") {
		f := strings.TrimSpace(raw)
		if f == "" {
			continue
		}
		if !models.ValidFolder(f) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid folder"})
			return
		}
		folders = append(folders, f)
	}
	if len(folders) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "folders required"})
		return
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil || limit < 1 || limit > maxProviderFolderMessages {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
		return
	}
	messages, err := h.EmailSyncState.ListProviderFolderMessages(c.Request.Context(), userID, emailID, folders, limit)
	if err != nil {
		errs.CaptureException(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list stored messages"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": messages})
}
