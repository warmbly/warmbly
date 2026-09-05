package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/instancecheck"
	"github.com/warmbly/warmbly/internal/app/instanceconfig"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
)

// The Instance surface answers the two questions a self-hoster cannot answer
// from inside the product today: what did my environment actually resolve to,
// and what is wrong with this deployment right now.

// AdminInstanceConfig returns the resolved effective configuration.
//
// Read-only by design: the environment is authoritative, so no API ever writes
// a key on this page. Sensitive values never leave the process; they carry a
// short fingerprint instead, which is enough to confirm two services hold the
// same AUTH_SECRET without disclosing either.
func (h *Handler) AdminInstanceConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"entries": instanceconfig.Entries(h.InstanceRuntime)})
}

// AdminInstanceLimits returns the effective product limits. Every value is a
// compiled constant, so this exists for visibility, not for editing.
func (h *Handler) AdminInstanceLimits(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"groups": instanceconfig.Limits()})
}

// AdminInstanceHealth runs the setup and health checks and returns only the
// findings. Checks run in parallel with a per-check timeout, so this is safe
// to poll, and a check whose input is unavailable is skipped rather than
// reported as a failure.
func (h *Handler) AdminInstanceHealth(c *gin.Context) {
	registry := h.InstanceChecks
	if registry == nil {
		// Still answer with the environment-only checks rather than an empty
		// page: a page that says nothing is indistinguishable from a healthy one.
		registry = instancecheck.New(instancecheck.Deps{
			Runtime:   h.InstanceRuntime,
			Transport: h.MailTransportRef,
		})
	}

	checks, summary := registry.Run(c.Request.Context(), instancecheck.Input{
		Host:      c.Request.Host,
		Origin:    c.GetHeader("Origin"),
		Forwarded: c.GetHeader("X-Forwarded-For") != "",
	})

	c.JSON(http.StatusOK, gin.H{"checks": checks, "summary": summary})
}

// AdminGetInstanceSettings returns the database-backed settings document.
//
// Notification channel targets and secrets never leave the process in full: a
// chat webhook URL is a bearer credential, so it reads back as a recognisable
// preview and the panel sends the preview (or nothing) to mean "unchanged".
func (h *Handler) AdminGetInstanceSettings(c *gin.Context) {
	if h.InstanceSettings == nil {
		c.JSON(http.StatusOK, redactSettings(instancesettings.Defaults()))
		return
	}
	c.JSON(http.StatusOK, redactSettings(h.InstanceSettings.Get(c.Request.Context())))
}

func redactSettings(doc instancesettings.Document) instancesettings.Document {
	doc.Notifications.Channels = doc.Notifications.RedactedChannels()
	return doc
}

// AdminPutInstanceSettings validates, clamps and stores the settings document.
// Absent fields keep their stored value, so a client that does not know about
// a key cannot clear it.
func (h *Handler) AdminPutInstanceSettings(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if h.InstanceSettings == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "Instance settings are not available on this deployment."))
		return
	}

	var patch instancesettings.Patch
	if err := c.ShouldBindJSON(&patch); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	doc, err := h.InstanceSettings.Put(c.Request.Context(), patch, adminID)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}

	if h.AdminService != nil {
		h.AdminService.LogAdminAction(
			c.Request.Context(),
			*adminID,
			"update_instance_settings",
			"instance",
			nil,
			instanceSettingsAuditDetails(doc),
			c.ClientIP(),
			c.Request.UserAgent(),
		)
	}

	c.JSON(http.StatusOK, redactSettings(doc))
}

func instanceSettingsAuditDetails(doc instancesettings.Document) map[string]any {
	details := map[string]any{
		"invitations_links_enabled":          doc.Invitations.LinksEnabled,
		"invitations_ttl_hours":              doc.Invitations.TTLHours,
		"access_allow_invited_signup":        doc.Access.AllowInvitedSignup,
		"sync_backfill_days":                 doc.Sync.BackfillDays,
		"sync_backfill_messages":             doc.Sync.BackfillMessages,
		"sync_daily_messages_mailbox":        doc.Sync.DailyMessagesPerMailbox,
		"sync_daily_messages_org":            doc.Sync.DailyMessagesPerOrg,
		"retention_engagement_event_days":    doc.Retention.EngagementEventDays,
		"retention_form_event_days":          doc.Retention.FormEventDays,
		"retention_audit_log_days":           doc.Retention.AuditLogDays,
		"deliverability_enforce_domain_auth": doc.Deliverability.EnforceDomainAuth,
		"deliverability_auth_grace_hours":    doc.Deliverability.AuthGraceHours,
		"notification_channels":              len(doc.Notifications.Channels),
	}
	return details
}
