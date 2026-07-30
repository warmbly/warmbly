package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// OnboardingOAuthStartRequest starts an OAuth round trip for a Gmail or Outlook account.
type OnboardingOAuthStartRequest struct {
	Provider string `json:"provider"`
}

// OnboardingOAuthFinishRequest carries the authorization code + state back from the provider.
type OnboardingOAuthFinishRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// OnboardingSMTPIMAPRequest connects an SMTP/IMAP mailbox in a single call.
type OnboardingSMTPIMAPRequest struct {
	Email string          `json:"email"`
	Name  string          `json:"name"`
	SMTP  *models.Service `json:"smtp"`
	IMAP  *models.Service `json:"imap"`
}

// OnboardingOutlookSharedRequest connects a shared Microsoft 365 mailbox by
// cloning/reusing an already connected licensed Outlook delegate account. It
// performs a read-only Graph validation before persisting a distinct sender row.
type OnboardingOutlookSharedRequest struct {
	ParentEmailAccountID string `json:"parent_email_account_id"`
	Email                string `json:"email"`
	Name                 string `json:"name"`
}

// OnboardingOutlookAppOnlyRequest connects an approved tenant mailbox using
// Microsoft Graph application permissions. The server validates read-only Graph
// access before it persists the sender.
type OnboardingOutlookAppOnlyRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (h *Handler) StartEmailOAuth(c *gin.Context) {
	userID := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)

	var req OnboardingOAuthStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, xerr := h.EmailService.OAuthStart(c.Request.Context(), userID, orgID, models.InboxProvider(req.Provider))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) FinishEmailOAuth(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	var req OnboardingOAuthFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	acc, xerr := h.EmailService.OAuthFinish(c.Request.Context(), userIDStr, req.Code, req.State)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider": acc.Provider,
		"email":    acc.Email,
	})

	c.JSON(http.StatusCreated, acc)
}

func (h *Handler) ConnectEmailOutlookShared(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)

	var req OnboardingOutlookSharedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	parentID, err := uuid.Parse(req.ParentEmailAccountID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	acc, xerr := h.EmailService.OnboardOutlookShared(c.Request.Context(), userIDStr, orgID, &models.NewSharedOutlookMailboxAccount{
		OrganizationID:       orgID,
		ParentEmailAccountID: parentID,
		Email:                req.Email,
		Name:                 req.Name,
	})
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider":                "outlook",
		"email":                   acc.Email,
		"parent_email_account_id": parentID.String(),
		"shared_mailbox":          "true",
	})

	c.JSON(http.StatusCreated, acc)
}

func (h *Handler) ConnectEmailOutlookAppOnly(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)

	var req OnboardingOutlookAppOnlyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	acc, xerr := h.EmailService.OnboardOutlookAppOnly(c.Request.Context(), userIDStr, orgID, &models.NewOutlookAppOnlyMailboxAccount{
		OrganizationID: orgID,
		Email:          req.Email,
		Name:           req.Name,
	})
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider":       "outlook",
		"email":          acc.Email,
		"graph_app_only": "true",
	})

	c.JSON(http.StatusCreated, acc)
}

func (h *Handler) ConnectEmailSMTPIMAP(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)

	var req OnboardingSMTPIMAPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	acc, xerr := h.EmailService.OnboardSMTPIMAP(c.Request.Context(), userIDStr, orgID, &models.NewSMTPIMAPAccount{
		Email: req.Email,
		Name:  req.Name,
		SMTP:  req.SMTP,
		IMAP:  req.IMAP,
	})
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider": "smtp_imap",
		"email":    acc.Email,
	})

	c.JSON(http.StatusCreated, acc)
}
