package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/agencyhome"
	"github.com/warmbly/warmbly/internal/errx"
)

// agencyHomeCaller is agencyCaller for the agency home routes.
func (h *Handler) agencyHomeCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.AgencyHomeService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "agencies are not available on this instance"))
		return uuid.Nil, uuid.Nil, false
	}
	return h.agencyCaller(c)
}

// GetAgencyOverview is GET /agency/:agencyId/overview: every client, problems first.
func (h *Handler) GetAgencyOverview(c *gin.Context) {
	userID, agencyID, ok := h.agencyHomeCaller(c)
	if !ok {
		return
	}
	o, xerr := h.AgencyHomeService.Overview(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, o)
}

// queryUUIDs reads a comma-separated id parameter.
func queryUUIDs(c *gin.Context, name string) ([]uuid.UUID, bool) {
	var out []uuid.UUID
	for _, part := range strings.Split(c.Query(name), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := uuid.Parse(part)
		if err != nil {
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
}

// ListAgencyInbox is GET /agency/:agencyId/inbox: conversations across the clients whose inbox the caller reads.
func (h *Handler) ListAgencyInbox(c *gin.Context) {
	defer boundUniboxRead(c)()
	userID, agencyID, ok := h.agencyHomeCaller(c)
	if !ok {
		return
	}
	clients, ok := queryUUIDs(c, "client")
	if !ok {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid client id"))
		return
	}
	page, xerr := h.AgencyHomeService.Inbox(c.Request.Context(), agencyID, userID, agencyhome.InboxParams{
		Clients:    clients,
		Unread:     c.Query("unread") == "true",
		NeedsReply: c.Query("needs_reply") == "true",
		Positive:   c.Query("positive") == "true",
		Search:     c.Query("q"),
		Cursor:     c.Query("cursor"),
		Limit:      c.Query("limit"),
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, page)
}

func agencyClientParam(c *gin.Context) (uuid.UUID, bool) {
	orgID, err := uuid.Parse(c.Query("organization_id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "organization_id is required"))
		return uuid.Nil, false
	}
	return orgID, true
}

// GetAgencyInboxThread is GET /agency/:agencyId/inbox/thread: one client conversation, read-only.
func (h *Handler) GetAgencyInboxThread(c *gin.Context) {
	defer boundUniboxRead(c)()
	userID, agencyID, ok := h.agencyHomeCaller(c)
	if !ok {
		return
	}
	orgID, ok := agencyClientParam(c)
	if !ok {
		return
	}
	res, xerr := h.AgencyHomeService.Thread(c.Request.Context(), agencyID, userID, orgID, c.Query("thread_id"), c.Query("cursor"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, res)
}

// GetAgencyInboxMessage is GET /agency/:agencyId/inbox/messages/:messageId: one message body, left unread.
func (h *Handler) GetAgencyInboxMessage(c *gin.Context) {
	defer boundUniboxRead(c)()
	userID, agencyID, ok := h.agencyHomeCaller(c)
	if !ok {
		return
	}
	orgID, ok := agencyClientParam(c)
	if !ok {
		return
	}
	messageID, err := uuid.Parse(c.Param("messageId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid message id"))
		return
	}
	msg, xerr := h.AgencyHomeService.Message(c.Request.Context(), agencyID, userID, orgID, messageID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, msg)
}
