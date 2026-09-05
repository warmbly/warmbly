package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
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

func (h *Handler) StartEmailOAuth(c *gin.Context) {
	userID := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

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

	acc, reauthed, xerr := h.EmailService.OAuthFinish(c.Request.Context(), userIDStr, req.Code, req.State)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	// A reauth round trip updated an existing mailbox rather than creating one.
	action, status := models.AuditActionConnect, http.StatusCreated
	if reauthed {
		action, status = models.AuditActionUpdate, http.StatusOK
	}
	h.auditOrg(c, action, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider": acc.Provider,
		"email":    acc.Email,
	})

	c.JSON(status, acc)
}

// ReauthEmailOAuth starts an OAuth round trip that renews the tokens of an
// existing mailbox after the provider invalidated them (issue #274). The
// finish leg is the ordinary FinishEmailOAuth.
func (h *Handler) ReauthEmailOAuth(c *gin.Context) {
	userID := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	resp, xerr := h.EmailService.OAuthReauth(c.Request.Context(), userID, orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// OnboardingSMTPIMAPCredentials carries replacement credentials for an
// existing SMTP/IMAP mailbox; the account fields never change on a reauth.
type OnboardingSMTPIMAPCredentials struct {
	SMTP *models.Service `json:"smtp"`
	IMAP *models.Service `json:"imap"`
}

// UpdateEmailSMTPIMAP replaces an SMTP/IMAP mailbox's credentials after a
// password change, validating them live before storing, and reactivates it.
func (h *Handler) UpdateEmailSMTPIMAP(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var req OnboardingSMTPIMAPCredentials
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	acc, xerr := h.EmailService.UpdateSMTPIMAPCredentials(c.Request.Context(), orgID, id, &models.SmtpImap{
		SMTP: req.SMTP,
		IMAP: req.IMAP,
	})
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &acc.ID, nil, map[string]string{
		"provider": "smtp_imap",
		"email":    acc.Email,
	})

	c.JSON(http.StatusOK, acc)
}

func (h *Handler) ConnectEmailSMTPIMAP(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

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

// OnboardingSMTPIMAPBulkRequest carries up to config.MailboxBulkBatchMax rows.
type OnboardingSMTPIMAPBulkRequest struct {
	Accounts []OnboardingSMTPIMAPRequest `json:"accounts"`
}

// ConnectEmailSMTPIMAPBulk is POST /emails/onboarding/smtp-imap/bulk: the
// dashboard's CSV import streams a file through it in batches. The answer is
// always 200 with a per-row status, so one bad row never hides the others.
// Re-sending a batch is safe: a mailbox that is already connected is skipped.
func (h *Handler) ConnectEmailSMTPIMAPBulk(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

	var req OnboardingSMTPIMAPBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	if len(req.Accounts) == 0 {
		errx.Handle(c, errx.New(errx.BadRequest, "accounts must carry at least one row"))
		return
	}
	if len(req.Accounts) > config.MailboxBulkBatchMax {
		errx.Handle(c, errx.New(errx.BadRequest, fmt.Sprintf("accounts may carry at most %d rows per request", config.MailboxBulkBatchMax)))
		return
	}

	rows := make([]models.NewSMTPIMAPAccount, len(req.Accounts))
	for i, a := range req.Accounts {
		rows[i] = models.NewSMTPIMAPAccount{Email: a.Email, Name: a.Name, SMTP: a.SMTP, IMAP: a.IMAP}
	}

	result := h.EmailService.OnboardSMTPIMAPBulk(c.Request.Context(), userIDStr, orgID, rows)
	for _, r := range result.Data {
		if r.Status != models.MailboxBulkConnected || r.ID == nil {
			continue
		}
		h.auditOrg(c, models.AuditActionConnect, models.AuditEntityEmailAccount, r.ID, nil, map[string]string{
			"provider": "smtp_imap",
			"email":    r.Email,
			"bulk":     "true",
		})
	}

	c.JSON(http.StatusOK, result)
}

// GetMailboxAllowance is GET /emails/allowance: how many mailboxes the
// workspace holds, how many it may hold and why, and any open request for
// more. The dashboard reads it before a connect so the answer is never a
// surprise after the credentials were typed.
func (h *Handler) GetMailboxAllowance(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	a, xerr := h.OrganizationService.MailboxAllowance(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, a)
}
