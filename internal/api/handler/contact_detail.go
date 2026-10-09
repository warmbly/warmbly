package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// GetContact returns the hydrated contact 360 payload. Used by the
// slide-over on open to render the Overview / engagement-summary tab
// in a single round-trip. Organization is optional — when the caller
// has none selected we still return core + engagement, just without
// org-scoped suppression / complaint counts.
func (h *Handler) GetContact(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	orgID := middleware.GetOrganizationID(c)

	detail, xerr := h.ContactService.GetDetail(c.Request.Context(), userID, orgID, contactID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, detail)
}

// LookupContactByEmail resolves a sender address to a contact in the caller's
// organization, for the unibox CRM panel. Returns {"contact": null} with 200
// when no contact matches, so an unknown sender renders a clean empty state
// rather than a 404. thread_id resolves a sender from an alias to the lead.
func (h *Handler) LookupContactByEmail(c *gin.Context) {
	email := strings.TrimSpace(c.Query("email"))
	// Senders often arrive as "Display Name <addr@example.com>" (the unibox
	// stores from_addr that way). Extract the bare address so it matches the
	// contact's email, otherwise every named sender resolves to "not found".
	if i := strings.LastIndex(email, "<"); i != -1 {
		if j := strings.Index(email[i:], ">"); j != -1 {
			email = strings.TrimSpace(email[i+1 : i+j])
		}
	}
	threadID := strings.TrimSpace(c.Query("thread_id"))
	if email == "" && threadID == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "email or thread_id is required"))
		return
	}
	var accountID *uuid.UUID
	if v := strings.TrimSpace(c.Query("account_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			errx.Handle(c, errx.ErrUuid)
			return
		}
		if !middleware.EmailAccountAllowed(c, id) {
			errx.Handle(c, errx.New(errx.Forbidden, "email account is not allowed for this API key"))
			return
		}
		accountID = &id
	}

	orgID := middleware.GetOrganizationID(c)

	thread := models.ContactLookupThread{ID: threadID, AccountID: accountID, AllowedAccounts: middleware.AllowedEmailAccounts(c)}
	res, xerr := h.ContactService.LookupSender(c.Request.Context(), orgID, email, thread)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, res)
}

// ListContactEmails returns one row per email we sent (or tried to
// send) to the contact. Cursor pagination keyed on the task ID.
func (h *Handler) ListContactEmails(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	// The cursor for sent-email pagination is the (created_at, task_id)
	// pair from the last row of the previous page. Both must be present
	// or we skip the cursor and start fresh.
	var beforeAt *time.Time
	var beforeID *uuid.UUID
	if v := c.Query("before_at"); v != "" {
		if t, perr := time.Parse(time.RFC3339Nano, v); perr == nil {
			beforeAt = &t
		}
	}
	if v := c.Query("before_id"); v != "" {
		if id, perr := uuid.Parse(v); perr == nil {
			beforeID = &id
		}
	}

	res, xerr := h.ContactService.ListSentEmails(c.Request.Context(), *orgID, contactID, limit, beforeAt, beforeID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, res)
}

// ListContactCampaignStates returns, for every campaign the contact is a
// lead of, the flow with this contact's progress and a scheduler-derived next
// action. Org-scoped, like the timeline.
func (h *Handler) ListContactCampaignStates(c *gin.Context) {
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	states, xerr := h.ContactService.CampaignStates(c.Request.Context(), *orgID, contactID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, models.ContactCampaignStatesResult{Data: states})
}

// ListContactTimeline returns the merged activity feed for a contact.
// Suppression / deliverability / reply / note events are org-scoped
// and require a selected organization; we 400 if there isn't one to
// avoid silently returning a misleadingly thin feed.
func (h *Handler) ListContactTimeline(c *gin.Context) {
	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	limit := 50
	if raw := c.Query("limit"); raw != "" {
		l, err := strconv.Atoi(raw)
		if err != nil || l < 1 || l > 200 {
			errx.Handle(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		limit = l
	}

	// The page resumes after an opaque (at, source, id) position. The older
	// `before` timestamp is still accepted and maps onto the same keyset at
	// rank zero, which admits exactly the events strictly older than it.
	var cursor *models.ContactTimelineKey
	if raw := c.Query("cursor"); raw != "" {
		at, source, id, xerr := paging.DecodeMergedCursor(raw)
		if xerr != nil {
			errx.Handle(c, xerr)
			return
		}
		if !models.ContactTimelineSource(source).Valid() {
			errx.Handle(c, errx.New(errx.BadRequest, "invalid cursor"))
			return
		}
		cursor = &models.ContactTimelineKey{At: at, Source: models.ContactTimelineSource(source), ID: id}
	} else if raw := c.Query("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "before must be an RFC 3339 timestamp"))
			return
		}
		cursor = &models.ContactTimelineKey{At: t}
	}

	res, xerr := h.ContactService.ListTimeline(c.Request.Context(), *orgID, contactID, limit, cursor)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, res)
}
