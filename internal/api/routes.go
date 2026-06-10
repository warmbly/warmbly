package api

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/handler/grouph"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/models"
)

func Run(
	h *handler.Handler,
	m *middleware.Handler,
	oidcm *middleware.OidcHandler,
	addr, ginMode string,
	allowedOrigins []string,
) *gin.Engine {
	gin.SetMode(ginMode)

	r := gin.Default()
	r.Use(middleware.RequestIDMiddleware())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Public webhook for GitHub release events. Auth comes from
	// X-Hub-Signature-256 (HMAC-SHA256 with RELEASES_WEBHOOK_SECRET).
	r.POST("/webhooks/github/releases", h.GithubReleasesWebhook)

	// Public inbound webhooks for third-party integrations. Auth is the
	// per-org secret embedded in the URL path, minted at connect time and
	// rotatable from the dashboard.
	r.POST("/api/v1/integrations/inbound/calendly/:secret", h.InboundCalendly)
	r.POST("/api/v1/integrations/inbound/cal-com/:secret", h.InboundCalCom)

	// Public worker enrollment. The one-time enrollment token is the
	// credential; successful exchange returns a dotenv file for the installer
	// and consumes the token.
	r.GET("/worker-install.sh", h.ServeWorkerInstaller)
	r.POST("/api/v1/workers/enroll", h.EnrollWorker)

	// Public OAuth-bouncer pages used by the mailbox onboarding popup.
	// The provider redirects here; the page postMessages the code/state
	// back to the SPA opener which then calls /emails/onboarding/oauth/finish.
	r.GET("/addresses/google/callback", h.EmailOAuthCallbackGmail)
	r.GET("/addresses/outlook/callback", h.EmailOAuthCallbackOutlook)

	// Public OAuth callback bouncer for third-party integrations (HubSpot,
	// Slack, Google, Pipedrive, …). The provider redirects here; the page
	// postMessages code+state to the SPA opener, which calls oauth/finish.
	r.GET("/integrations/oauth/callback", h.IntegrationOAuthCallback)

	// Public List-Unsubscribe endpoint (RFC 8058). GET = recipient clicks the
	// link; POST = mailbox provider's one-click (body List-Unsubscribe=One-Click).
	// Both suppress the recipient org-wide. Unauthenticated by design.
	r.GET("/unsubscribe", h.Unsubscribe)
	r.POST("/unsubscribe", h.Unsubscribe)

	// Internal backend-to-backend endpoints. Workers call these instead of
	// touching Postgres directly, per the no-direct-data-services rule in
	// CLAUDE.md. Auth: shared bearer token (INTERNAL_API_TOKEN).
	internal := r.Group("/api/v1/internal")
	internal.Use(m.InternalAuthMiddleware())
	{
		internal.GET("/dek/:orgID", h.InternalGetDEK)
		internal.PUT("/dek/:orgID", h.InternalPutDEK)
		internal.DELETE("/dek/:orgID", h.InternalDeleteDEK)

		// Worker mailbox-sync messageId -> internal email map (replaces the
		// former DynamoDB EmailMessageData table). Workers read/write it here.
		internal.GET("/email-message-map", h.InternalGetEmailMessageMap)
		internal.PUT("/email-message-map", h.InternalPutEmailMessageMap)
		internal.DELETE("/email-message-map", h.InternalDeleteEmailMessageMap)

		// Worker bootstrap config + heartbeat. Workers POST their identity
		// on boot (worker_id + bind_ip + tag) and pull their runtime config
		// instead of carrying it all in the install-time env file.
		internal.GET("/worker/config", h.InternalWorkerConfig)
		internal.POST("/worker/heartbeat", h.InternalWorkerHeartbeat)
	}

	corsConfig := cors.Config{
		AllowMethods: []string{"POST", "GET", "PUT", "PATCH", "OPTIONS", "DELETE"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
			"Idempotency-Key",
			"X-Request-Id",
		},
		ExposeHeaders: []string{
			"Content-Length",
			"X-Request-Id",
			"X-RateLimit-Limit",
			"X-RateLimit-Remaining",
			"X-RateLimit-Policy",
			"Retry-After",
		},
		MaxAge: 12 * time.Hour,
	}
	switch {
	case len(allowedOrigins) == 0 && ginMode != gin.ReleaseMode:
		corsConfig.AllowOrigins = []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"http://localhost:4173",
			"http://127.0.0.1:4173",
			"http://localhost:5173",
			"http://127.0.0.1:5173",
			"http://localhost:5174",
			"http://127.0.0.1:5174",
		}
		corsConfig.AllowCredentials = true
	case len(allowedOrigins) == 1 && allowedOrigins[0] == "*":
		corsConfig.AllowAllOrigins = true
		corsConfig.AllowCredentials = false
	default:
		corsConfig.AllowOrigins = allowedOrigins
		corsConfig.AllowCredentials = true
	}

	// In non-production builds, also accept the loopback / LAN / Tailscale
	// origins a developer might serve the dashboards from (e.g. reaching the API
	// over a Tailscale IP from another device) without enumerating every
	// host:port. AllowOriginFunc is only consulted when the explicit AllowOrigins
	// list above doesn't already match, and the middleware reflects the specific
	// origin back, so AllowCredentials keeps working. Release mode never sets it
	// and stays restricted to the explicit allowlist.
	if ginMode != gin.ReleaseMode && !corsConfig.AllowAllOrigins {
		corsConfig.AllowOriginFunc = devOriginAllowed
	}

	r.Use(cors.New(corsConfig))

	// Limit request body size to 10MB to prevent OOM
	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)
		c.Next()
	})

	auth := r.Group("/auth")
	{
		auth.POST("/login", h.LoginStart)
		auth.POST("/login/confirm", h.LoginConfirm)
		auth.POST("/register", h.RegistrationStart)
		auth.POST("/register/confirm", h.RegistrationConfirm)
		auth.POST("/refresh", h.RefreshToken)
		auth.POST("/reset-password", h.ResetPasswordStart)
		auth.POST("/reset-password/confirm", h.ResetPasswordConfirm)

		// Passkey (WebAuthn) sign-in is discoverable/usernameless: a passkey
		// is already strong auth, so it's a single step with no email OTP.
		// Public on purpose — there's no account context until the assertion
		// resolves, and the challenge + signature are the protection.
		auth.POST("/passkey/login/begin", h.PasskeyLoginBegin)
		auth.POST("/passkey/login/finish", h.PasskeyLoginFinish)

		// 2FA login challenge (PUBLIC): exchanges a single-use pending token +
		// TOTP/recovery code for a real session. Rate-limited in the service
		// (no user context here, so RateLimitMiddleware would be a no-op).
		auth.POST("/2fa/verify", h.TwoFAVerifyLogin)
	}

	protectedAuth := auth.Group("")
	protectedAuth.Use(m.AuthMiddleware())
	{
		protectedAuth.POST("/logout", h.Logout)
		protectedAuth.POST("/logout-all", h.LogoutAll)

		// Self-service session management. Scoped to the authenticated user;
		// per-id revoke can never touch another user's session.
		protectedAuth.GET("/sessions", h.SessionsList)
		protectedAuth.DELETE("/sessions", h.SessionRevokeOthers)
		protectedAuth.DELETE("/sessions/:id", h.SessionRevoke)

		protectedAuth.GET("/me", h.GetUser)
		protectedAuth.PATCH("/me", h.UpdateUserProfile)
		protectedAuth.PATCH("/me/onboarding", h.CompleteOnboarding)
		protectedAuth.POST("/me/avatar", h.UploadUserAvatar)
		protectedAuth.DELETE("/me/avatar", h.DeleteUserAvatar)

		// Notification preferences + in-app feed (user-scoped, no org gate).
		protectedAuth.GET("/me/notification-preferences", h.GetNotificationPreferences)
		protectedAuth.PUT("/me/notification-preferences", h.UpdateNotificationPreferences)
		protectedAuth.GET("/me/notifications", h.ListNotifications)
		protectedAuth.PUT("/me/notifications", h.MarkAllNotificationsRead)
		protectedAuth.POST("/me/notifications/:id/read", h.MarkNotificationRead)

		// 2FA enrollment + management (user-scoped, behind a live session).
		protectedAuth.GET("/2fa/status", h.TwoFAStatus)
		protectedAuth.POST("/2fa/enroll/start", h.TwoFAEnrollStart)
		protectedAuth.POST("/2fa/enroll/confirm", h.TwoFAEnrollConfirm)
		protectedAuth.DELETE("/2fa", h.TwoFADisable)

		// Passkey enrollment + management require an authenticated session.
		protectedAuth.POST("/passkey/register/begin", h.PasskeyRegisterBegin)
		protectedAuth.POST("/passkey/register/finish", h.PasskeyRegisterFinish)
		protectedAuth.GET("/passkey/credentials", h.PasskeyListCredentials)
		protectedAuth.PATCH("/passkey/credentials/:id", h.PasskeyRenameCredential)
		protectedAuth.DELETE("/passkey/credentials/:id", h.PasskeyDeleteCredential)
	}

	// JWT-only protected group: routes that are tied to a human session and
	// must never be reachable via a long-lived API key. This is the safety
	// boundary for billing, organization governance, websocket bootstrapping,
	// and the email onboarding flow (which writes user-encrypted secrets).
	jwtOnly := r.Group("")
	jwtOnly.Use(m.AuthMiddleware())

	// API-accessible protected group: routes that accept either a JWT or an
	// API key. CombinedAuthMiddleware sets the same context keys for both
	// auth types; APIKeyUsageMiddleware records one log row per API-key
	// request (JWT requests are skipped).
	protected := r.Group("")
	protected.Use(m.CombinedAuthMiddleware(), m.APIKeyUsageMiddleware(), m.IdempotencyMiddleware())
	{
		emails := protected.Group("/emails")
		emails.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			emails.GET("", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadEmails), h.EmailsSearch)
			emails.GET("/:id", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.GetEmail)
			emails.PATCH("/:id", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.UpdateEmail)
			emails.PATCH("/:id/track", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.UpdateEmailTrackingDomain)
			emails.POST("/:id/warmup/start", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.StartWarmup)
			emails.POST("/:id/warmup/pause", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.PauseWarmup)
			emails.POST("/:id/warmup/resume", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.ResumeWarmup)
			emails.POST("/:id/warmup/stop", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.StopWarmup)
			emails.GET("/:id/auth-check", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.GetEmailAuthCheck)
			emails.POST("/verify", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadEmails), h.VerifyEmail)
			emails.GET("/:id/warmup/ban-status", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.GetWarmupBanStatus)
			emails.POST("/:id/warmup/appeal", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.SubmitWarmupAppeal)
			emails.DELETE("/:id", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), middleware.RequireAPIKeyEmailAccountParam("id"), h.DeleteEmail)
			emails.POST("/:id/send", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), middleware.RequireAPIKeyEmailAccountParam("id"), h.SendEmailFromAccount)
		}

		// Email onboarding is JWT-only — it writes user-encrypted refresh
		// tokens via the SPA popup flow and shouldn't be triggerable by an
		// API key with a long lifetime.
		onboardingEmails := jwtOnly.Group("/emails/onboarding")
		onboardingEmails.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			onboardingEmails.POST("/oauth/start", h.StartEmailOAuth)
			onboardingEmails.POST("/oauth/finish", h.FinishEmailOAuth)
			onboardingEmails.POST("/smtp-imap", h.ConnectEmailSMTPIMAP)
		}

		// Integration OAuth handshake is JWT-only — it writes user-encrypted
		// provider tokens via the SPA popup flow, same as mailbox onboarding.
		integrationsOAuth := jwtOnly.Group("/integrations/oauth")
		integrationsOAuth.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			integrationsOAuth.POST("/start", h.StartIntegrationOAuth)
			integrationsOAuth.POST("/finish", h.FinishIntegrationOAuth)
			integrationsOAuth.POST("/reauth/:id", h.ReauthIntegration)
		}

		// Template preview/validation (no campaign id; can't be a static sibling
		// of /campaigns/:id, so it lives one level up). Renders against a sample
		// contact — read-level access, no side effects.
		protected.POST("/campaign-template-preview", m.RequireOrganization(), m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.PreviewCampaignTemplate)

		campaigns := protected.Group("/campaigns")
		campaigns.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			campaigns.GET("", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.SearchCampaigns)
			campaigns.POST("", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.CreateCampaign)
			campaigns.GET("/:id", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.GetCampaign)
			campaigns.PATCH("/:id", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.UpdateCampaign)
			campaigns.DELETE("/:id", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.DeleteCampaign)

			// Advanced campaign controls
			campaigns.GET("/:id/advanced", m.RequireOrganization(), m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.GetCampaignAdvancedSettings)
			campaigns.PATCH("/:id/advanced", m.RequireOrganization(), m.RequireAccess(models.PermManageSettings, models.APIPermWriteCampaigns), h.UpdateCampaignAdvancedSettings)
			campaigns.GET("/:id/ab-variants", m.RequireOrganization(), m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.ListCampaignABVariants)
			campaigns.POST("/:id/ab-variants", m.RequireOrganization(), m.RequireAccess(models.PermManageSettings, models.APIPermWriteCampaigns), h.CreateCampaignABVariant)
			campaigns.PATCH("/:id/ab-variants/:variantId", m.RequireOrganization(), m.RequireAccess(models.PermManageSettings, models.APIPermWriteCampaigns), h.UpdateCampaignABVariant)
			campaigns.DELETE("/:id/ab-variants/:variantId", m.RequireOrganization(), m.RequireAccess(models.PermManageSettings, models.APIPermWriteCampaigns), h.DeleteCampaignABVariant)
			campaigns.GET("/:id/attachments", m.RequireOrganization(), m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.ListCampaignAttachments)
			campaigns.POST("/:id/attachments", m.RequireOrganization(), m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.UploadCampaignAttachment)
			campaigns.DELETE("/:id/attachments/:attachmentId", m.RequireOrganization(), m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.DeleteCampaignAttachment)
			campaigns.POST("/:id/preflight", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.RunCampaignPreflight)
			campaigns.GET("/:id/ab-analysis", m.RequireOrganization(), m.RequireAccess(models.PermViewAnalytics, models.APIPermReadAnalytics), h.GetCampaignABAnalysis)
			campaigns.POST("/:id/test-email", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.SendTestEmail)

			// Campaign start/stop
			campaigns.POST("/:id/start", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.StartCampaign)
			campaigns.POST("/:id/stop", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.StopCampaign)
			campaigns.GET("/:id/logs", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.GetCampaignLogs)

			// Explicit sender pool (rotation/weighting).
			campaigns.GET("/:id/senders", m.RequireOrganization(), m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.ListCampaignSenders)
			campaigns.PUT("/:id/senders", m.RequireOrganization(), m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.ReplaceCampaignSenders)

			// Campaign-scoped tracking-domain verification.
			campaigns.POST("/:id/tracking-domain/verify", m.RequireOrganization(), m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.VerifyCampaignTrackingDomain)

			sequences := campaigns.Group("/:id/sequences")
			{
				sequences.GET("", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.GetSequences)
				sequences.POST("", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.CreateSequence)
				sequences.PATCH("/:sid", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.UpdateSequence)
				sequences.DELETE("/:sid", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.DeleteSequence)
			}
		}

		generation := protected.Group("/generation")
		generation.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			generation.POST("/write", m.RequireOrganization(), m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.GenerateWriting)
		}

		contacts := protected.Group("/contacts")
		contacts.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			contacts.POST("/search", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.SearchContacts)
			contacts.POST("", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.AddContacts)
			contacts.DELETE("", m.RequireAccess(models.PermManageContacts, models.APIPermBulkContacts), h.DeleteContactBulk)
			contacts.PATCH("", m.RequireAccess(models.PermManageContacts, models.APIPermBulkContacts), h.UpdateContactBulk)
			// Import + export power-tools. Read-only export gates on
			// ReadContacts; the import endpoints write and so use the
			// stricter Write/Bulk scopes that the rest of the contact
			// write paths already use.
			contacts.POST("/export", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ExportContacts)
			contacts.POST("/import/preview", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.ImportPreviewContacts)
			contacts.POST("/import/commit", m.RequireAccess(models.PermManageContacts, models.APIPermBulkContacts), h.ImportCommitContacts)
			contacts.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.UpdateContact)
			contacts.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.DeleteContact)

			// Resolve a sender address to a contact (unibox CRM panel).
			// Registered before /:id so the fixed path wins over the catch-all.
			contacts.GET("/lookup", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.LookupContactByEmail)

			// Contact 360 view: hydrated detail, every email sent to
			// the contact, and the merged activity timeline.
			contacts.GET("/:id", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.GetContact)
			contacts.GET("/:id/emails", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ListContactEmails)
			contacts.GET("/:id/timeline", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ListContactTimeline)

			// CRM: Notes & Activities (under contacts)
			contacts.GET("/:id/notes", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ListContactNotes)
			contacts.POST("/:id/notes", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.CreateContactNote)
			contacts.PATCH("/:id/notes/:noteId", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.UpdateContactNote)
			contacts.DELETE("/:id/notes/:noteId", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.DeleteContactNote)
			contacts.GET("/:id/activities", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ListContactActivities)
			contacts.GET("/:id/deals", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetDealsByContact)
		}

		// Group endpoints map to the resources they organize: campaign
		// folders, email-account tags, and contact categories.
		grouph.New(protected, h.FolderService, h.AuditService, "folders", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns))
		grouph.New(protected, h.TagService, h.AuditService, "tags", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails))
		grouph.New(protected, h.CategoryService, h.AuditService, "categories", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts))

		unibox := protected.Group("/unibox")
		unibox.Use(m.RateLimitMiddleware(models.RateLimitRead))
		{
			unibox.GET("", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUniboxIncoming)
			unibox.GET("/count", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUnseenCount)
			unibox.GET("/overview", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUniboxOverview)
			unibox.GET("/thread", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUniboxThread)

			// Conversation labels — read the set on a thread, or replace
			// it wholesale (idempotent PUT). Registered before /:id so the
			// fixed path wins over the catch-all.
			unibox.GET("/thread/labels", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUniboxThreadLabels)
			unibox.PUT("/thread/labels", m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.SetUniboxThreadLabels)

			unibox.PATCH("/seen", m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.UniboxMarkSeen)
			unibox.POST("/reply", m.RequireOrganization(), m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.UniboxReply)

			// Snoozes — POST/DELETE on a thread, GET lists active ones.
			unibox.GET("/snoozes", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.ListUniboxSnoozes)
			unibox.POST("/snooze", m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.CreateUniboxSnooze)
			unibox.DELETE("/snooze", m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.DeleteUniboxSnooze)

			// Scheduled-sends review + cancel. DELETE is DB-only —
			// we don't pay Cloud Tasks to delete the queued task; the
			// handler short-circuits on cancelled status when it fires.
			unibox.GET("/scheduled", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.ListUniboxScheduled)
			unibox.DELETE("/scheduled/:task_id", m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.CancelUniboxScheduled)

			// Keep /:id last — gin treats it as a catch-all so any
			// fixed-name routes (above) must register first.
			unibox.GET("/:id", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUniboxEmail)
		}

		// API key management. JWT users need PermManageAPIKeys; API keys
		// need the APIPermAPIKeys self-service bit. This lets an integration
		// rotate its own keys without going through the dashboard.
		apiKeys := protected.Group("/api-keys")
		apiKeys.Use(m.RequireOrganization(), m.RequireAccess(models.PermManageAPIKeys, models.APIPermAPIKeys))
		apiKeys.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			apiKeys.GET("", h.ListAPIKeys)
			apiKeys.POST("", h.CreateAPIKey)
			apiKeys.GET("/permissions", h.ListAPIPermissions)
			apiKeys.GET("/usage/summary", h.GetAPIKeyUsageSummary)
			apiKeys.GET("/usage/analytics", h.GetAPIKeyAnalytics)
			apiKeys.GET("/:id", h.GetAPIKey)
			apiKeys.PATCH("/:id", h.UpdateAPIKey)
			apiKeys.DELETE("/:id", h.RevokeAPIKey)
			apiKeys.GET("/:id/analytics", h.GetAPIKeyAnalytics)
			apiKeys.GET("/:id/logs", h.ListAPIKeyUsageLogs)
		}

		// Analytics endpoints
		analytics := protected.Group("/analytics")
		analytics.Use(m.RateLimitMiddleware(models.RateLimitAnalytics), m.RequireAccess(models.PermViewAnalytics, models.APIPermReadAnalytics))
		{
			analytics.GET("/dashboard", h.GetDashboardAnalytics)
			analytics.GET("/deliverability", m.RequireOrganization(), h.GetDeliverabilityDashboard)
			analytics.GET("/warmup", h.GetWarmupAnalytics)
			analytics.GET("/campaigns/compare", h.CompareCampaigns)
			analytics.GET("/campaigns/:id", h.GetCampaignAnalytics)
			analytics.GET("/campaigns/:id/daily", h.GetCampaignDailyStats)
			analytics.GET("/campaigns/:id/hourly", h.GetCampaignHourlyStats)
			analytics.GET("/accounts", h.GetAllAccountStatuses)
			analytics.GET("/accounts/:id", h.GetAccountStatus)
			analytics.GET("/usage", h.GetUsageOverview)
		}

		// Audit logs
		auditLogs := protected.Group("/audit-logs")
		auditLogs.Use(m.RateLimitMiddleware(models.RateLimitRead), m.RequireAccess(models.PermViewAnalytics, models.APIPermReadAuditLogs))
		{
			auditLogs.GET("", h.GetAuditLogs)
		}

		// Realtime websocket bootstrap is JWT-only — the websocket itself
		// has its own session-based auth.
		realtime := jwtOnly.Group("/realtime")
		{
			realtime.GET("/info", h.GetRealtimeInfo)
		}

		// Advanced outreach controls (org-scoped)
		outreach := protected.Group("/outreach")
		outreach.Use(m.RequireOrganization(), m.RequireAccess(models.PermManageSettings, models.APIPermWriteCampaigns))
		{
			outreach.GET("/settings", h.GetOutreachSettings)
			outreach.PATCH("/settings", h.UpdateOutreachSettings)
		}

		// Deliverability event ingestion (org-scoped). API-key callable so
		// downstream pipelines (e.g. SES bounce processors) can post events
		// without a human in the loop.
		deliverability := protected.Group("/deliverability")
		deliverability.Use(m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermWriteCampaigns))
		{
			deliverability.POST("/events", h.IngestDeliverabilityEvent)
		}

		// Task dead letter operations (org-scoped). Requires SendCampaigns
		// because a replay actually re-dispatches mail.
		taskOps := protected.Group("/tasks")
		taskOps.Use(m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns))
		{
			taskOps.GET("/dlq", h.ListTaskDeadLetters)
			taskOps.POST("/dlq/:id/replay", h.ReplayTaskDeadLetter)
		}

		// Customer-facing webhooks (org-scoped).
		webhooks := protected.Group("/webhooks")
		webhooks.Use(m.RequireOrganization(), m.RequireAccess(models.PermManageSettings, models.APIPermWebhooks), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			webhooks.GET("", h.ListWebhookEndpoints)
			webhooks.POST("", h.CreateWebhookEndpoint)
			webhooks.PATCH("/:id", h.UpdateWebhookEndpoint)
			webhooks.DELETE("/:id", h.DeleteWebhookEndpoint)
			webhooks.POST("/:id/rotate-secret", h.RotateWebhookSecret)
			webhooks.GET("/:id/deliveries", h.ListWebhookDeliveries)
		}

		// Third-party integrations (org-scoped). Reads are reachable by both
		// settings managers AND operational integration users (PermUseIntegrations)
		// so contextual integration actions show up everywhere they belong;
		// connecting + configuring stays gated on PermManageSettings. Pushing
		// records on demand is an operational action (PermUseIntegrations).
		integrations := protected.Group("/integrations")
		integrations.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			read := m.RequireAnyAccess(models.APIPermIntegrations, models.PermManageSettings, models.PermUseIntegrations)
			write := m.RequireAccess(models.PermManageSettings, models.APIPermIntegrations)
			operate := m.RequireAccess(models.PermUseIntegrations, models.APIPermIntegrations)

			integrations.GET("/catalog", read, h.ListIntegrationCatalog)
			integrations.GET("/connections", read, h.ListIntegrationConnections)
			integrations.POST("/connections", write, h.ConnectIntegration)
			integrations.GET("/connections/:id", read, h.GetIntegrationConnection)
			integrations.PATCH("/connections/:id/config", write, h.UpdateConnectionConfig)
			integrations.DELETE("/connections/:id", write, h.DisconnectIntegration)
			integrations.GET("/connections/:id/events", read, h.ListConnectionEventSubscriptions)
			integrations.POST("/connections/:id/events", write, h.CreateConnectionEventSubscription)
			integrations.DELETE("/connections/:id/events/:eventId", write, h.DeleteConnectionEventSubscription)
			integrations.GET("/connections/:id/field-mappings", read, h.ListConnectionFieldMappings)
			integrations.PUT("/connections/:id/field-mappings", write, h.ReplaceConnectionFieldMappings)
			integrations.GET("/connections/:id/runs", read, h.ListConnectionSyncRuns)
			integrations.GET("/connections/:id/webhook-secret", write, h.GetConnectionWebhookSecret)
			integrations.POST("/connections/:id/test", write, h.TestConnection)
			integrations.POST("/connections/:id/push", operate, h.PushContactsToIntegration)
			integrations.GET("/bookings", read, h.ListMeetingBookings)
		}

		// Meetings (org-scoped). Booked calls from connected scheduling
		// providers (Calendly / Cal.com), surfaced as a first-class CRM list.
		// Read-only and reachable by anyone who can view contacts.
		meetings := protected.Group("/meetings")
		meetings.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			meetingsRead := m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts)
			meetingsWrite := m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts)
			meetings.GET("", meetingsRead, h.SearchMeetings)
			meetings.GET("/summary", meetingsRead, h.MeetingsSummary)
			meetings.POST("", meetingsWrite, h.CreateMeeting)
			meetings.DELETE("/:id", meetingsWrite, h.DeleteMeeting)
		}

		// Automations (org-scoped). The visual flow builder: a trigger event +
		// action steps across integrations. Reads reachable by operational
		// integration users; creating/editing is a settings action.
		automations := protected.Group("/automations")
		automations.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			aread := m.RequireAnyAccess(models.APIPermIntegrations, models.PermManageSettings, models.PermUseIntegrations)
			awrite := m.RequireAccess(models.PermManageSettings, models.APIPermIntegrations)
			automations.GET("", aread, h.ListAutomations)
			automations.POST("", awrite, h.CreateAutomation)
			automations.GET("/:id", aread, h.GetAutomation)
			automations.PATCH("/:id", awrite, h.UpdateAutomation)
			automations.DELETE("/:id", awrite, h.DeleteAutomation)
			automations.POST("/:id/test", aread, h.TestAutomation)
			automations.GET("/:id/runs", aread, h.ListAutomationRuns)
		}

		// On-demand Google Sheets -> leads sync (org-scoped). A saved "sync
		// source" the user re-runs with "Sync now"; new rows create contacts and
		// existing rows (matched by email) update. Gated under the contacts
		// write permissions because it ultimately upserts contacts. The Google
		// account itself is connected via the existing /integrations/oauth flow
		// with provider "google_sheets".
		leadSync := protected.Group("/lead-sync")
		leadSync.Use(m.RequireOrganization(), m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			leadSync.GET("/google/connection", h.GetLeadSyncGoogleConnection)
			leadSync.POST("/google/spreadsheet", h.GetLeadSyncSpreadsheet)
			leadSync.POST("/google/preview", h.PreviewLeadSync)

			leadSync.GET("/sources", h.ListLeadSyncSources)
			leadSync.POST("/sources", h.CreateLeadSyncSource)
			leadSync.GET("/sources/:id", h.GetLeadSyncSource)
			leadSync.PATCH("/sources/:id", h.UpdateLeadSyncSource)
			leadSync.DELETE("/sources/:id", h.DeleteLeadSyncSource)
			leadSync.POST("/sources/:id/sync", h.SyncLeadSyncSourceNow)
		}

		// Warmup routing rules (org-scoped). Lets customers define
		// preferences for premium-pool partner selection — e.g. send
		// to Gmail recipients only from Google-classified senders.
		warmupRouting := protected.Group("/warmup/routing")
		warmupRouting.Use(m.RequireOrganization(), m.RequireAccess(models.PermManageSettings, models.APIPermWarmupRouting), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			warmupRouting.GET("", h.ListWarmupRoutingRules)
			warmupRouting.POST("", h.CreateWarmupRoutingRule)
			warmupRouting.PATCH("/:id", h.UpdateWarmupRoutingRule)
			warmupRouting.DELETE("/:id", h.DeleteWarmupRoutingRule)
		}

		// Reply templates (org-scoped)
		templates := protected.Group("/templates")
		templates.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			templates.GET("", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadTemplates), h.ListTemplates)
			templates.POST("", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.CreateTemplate)
			templates.PATCH("/reorder", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.ReorderTemplates)
			templates.GET("/:id", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadTemplates), h.GetTemplate)
			templates.PATCH("/:id", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.UpdateTemplate)
			templates.DELETE("/:id", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.DeleteTemplate)
			templates.POST("/:id/duplicate", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.DuplicateTemplate)
			templates.POST("/:id/render", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadTemplates), h.RenderTemplate)
			templates.POST("/score", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadTemplates), h.ScoreTemplateContent)
		}

		// CRM routes (require org)
		crmGroup := protected.Group("/crm")
		crmGroup.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			pipelines := crmGroup.Group("/pipelines")
			{
				pipelines.GET("", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.ListPipelines)
				pipelines.POST("", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.CreatePipeline)
				pipelines.GET("/:id", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetPipeline)
				pipelines.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.UpdatePipeline)
				pipelines.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.DeletePipeline)
				pipelines.POST("/:id/stages", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.CreateStage)
				pipelines.PATCH("/:id/stages/:stageId", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.UpdateStage)
				pipelines.DELETE("/:id/stages/:stageId", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.DeleteStage)
			}

			deals := crmGroup.Group("/deals")
			{
				deals.GET("", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.ListDeals)
				deals.POST("", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.CreateDeal)
				deals.POST("/search", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.SearchDeals)
				deals.POST("/summary", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.DealsSummary)
				deals.GET("/:id", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetDeal)
				deals.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.UpdateDeal)
				deals.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.DeleteDeal)
			}

			taskTypes := crmGroup.Group("/task-types")
			{
				taskTypes.GET("", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.ListTaskTypes)
				taskTypes.POST("", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.CreateTaskType)
				taskTypes.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.UpdateTaskType)
				taskTypes.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.DeleteTaskType)
			}

			crmTasks := crmGroup.Group("/tasks")
			{
				crmTasks.GET("", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.ListCRMTasks)
				crmTasks.POST("", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.CreateCRMTask)
				crmTasks.POST("/search", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.SearchCRMTasks)
				crmTasks.POST("/summary", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.TasksSummary)
				crmTasks.GET("/:id", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetCRMTask)
				crmTasks.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.UpdateCRMTask)
				crmTasks.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.DeleteCRMTask)
			}
		}

		// Teams — group existing org members into named teams (for CRM
		// ownership / routing). Built from members; managed by team managers.
		teamsGroup := protected.Group("/teams")
		teamsGroup.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			teamsGroup.GET("", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.ListTeams)
			teamsGroup.POST("", m.RequireAccess(models.PermManageTeam, models.APIPermWriteCRM), h.CreateTeam)
			teamsGroup.GET("/:id", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetTeam)
			teamsGroup.PATCH("/:id", m.RequireAccess(models.PermManageTeam, models.APIPermWriteCRM), h.UpdateTeam)
			teamsGroup.DELETE("/:id", m.RequireAccess(models.PermManageTeam, models.APIPermWriteCRM), h.DeleteTeam)
			teamsGroup.POST("/:id/members", m.RequireAccess(models.PermManageTeam, models.APIPermWriteCRM), h.AddTeamMember)
			teamsGroup.DELETE("/:id/members/:userId", m.RequireAccess(models.PermManageTeam, models.APIPermWriteCRM), h.RemoveTeamMember)
		}

		// Plans and timezones are essentially public reference data — auth
		// gates them only to avoid being scraped. Cheap to expose to keys.
		protected.GET("/plans", h.ListPlans)
		protected.GET("/timezones", h.GetTimezones)
	}

	// Sensitive routes below — JWT only. Organization governance, billing,
	// websocket bootstrap, danger-zone destructions, and pending invitations
	// all live here. None of these are reachable via an API key.
	{
		org := jwtOnly.Group("/organization")
		org.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			org.POST("", h.CreateOrganization)
			org.GET("", h.GetUserOrganizations)
			org.POST("/switch/:id", h.SwitchOrganization)

			org.GET("/current", h.GetCurrentOrganization)
			org.PATCH("/current", m.RequireOrganization(), m.RequirePermission(models.PermManageSettings), h.UpdateOrganization)
			org.GET("/current/limits", m.RequireOrganization(), h.GetOrganizationLimits)

			org.GET("/members", m.RequireOrganization(), h.GetMembers)
			org.POST("/members/invite", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.InviteMember)
			org.PATCH("/members/:id", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.UpdateMemberRole)
			org.DELETE("/members/:id", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.RemoveMember)

			org.GET("/invitations", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.GetPendingInvitations)
			org.DELETE("/invitations/:id", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.CancelInvitation)

			org.POST("/transfer-ownership", m.RequireOrganization(), m.RequirePermission(models.PermTransferOwnership), h.TransferOwnership)

			org.POST("/avatar", m.RequireOrganization(), h.UploadOrganizationAvatar)
			org.DELETE("/avatar", m.RequireOrganization(), h.DeleteOrganizationAvatar)

			org.GET("/current/danger-zone", m.RequireOrganization(), h.GetOrganizationDangerZone)
			org.POST("/current/danger-zone/delete", m.RequireOrganization(), h.ScheduleOrganizationDeletion)
			org.DELETE("/current/danger-zone/delete", m.RequireOrganization(), h.CancelOrganizationDeletion)

			// Customer-facing limit-increase requests. The "current
			// effective" value is computed server-side at submission
			// time so the org/admin can see what the user was looking
			// at when they asked.
			org.POST("/:orgId/limit-requests", h.SubmitLimitIncreaseRequest)
			org.GET("/:orgId/limit-requests", h.ListOrgLimitRequests)
		}

		// Cancel a pending limit request by id (submitter-only). Sits
		// outside the /organization group so the URL doesn't need
		// double-encoding of the org id.
		jwtOnly.DELETE("/limit-requests/:id", h.CancelLimitRequest)

		account := jwtOnly.Group("/me")
		{
			account.GET("/danger-zone", h.GetAccountDangerZone)
			account.POST("/danger-zone/delete", h.ScheduleAccountDeletion)
			account.DELETE("/danger-zone/delete", h.CancelAccountDeletion)
		}

		jwtOnly.GET("/invitations", h.GetMyPendingInvitations)
		jwtOnly.POST("/invitations/accept", h.AcceptInvitation)

		// Websocket bootstrap. The token returned here is single-session.
		jwtOnly.POST("/getaway", h.GenerateWebsocket)

		subscriptions := jwtOnly.Group("/subscription")
		subscriptions.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			subscriptions.GET("", h.GetSubscription)
			subscriptions.GET("/limits", h.GetSubscriptionLimits)
			subscriptions.GET("/trial", h.GetTrialStatus)
			subscriptions.GET("/features", h.GetFeatureStatus)
			subscriptions.POST("/checkout", h.CreateCheckoutSession)
			subscriptions.POST("/discount/validate", h.ValidateDiscountCode)
			subscriptions.POST("/portal", h.CreateBillingPortalSession)
			subscriptions.POST("/cancel", h.CancelSubscription)

			subscriptions.POST("/change-plan", m.RequireOrganization(), m.RequirePermission(models.PermManageBilling), h.ChangePlan)
			subscriptions.GET("/preview-change", m.RequireOrganization(), m.RequirePermission(models.PermManageBilling), h.PreviewPlanChange)

			subscriptions.POST("/enterprise-inquiry", h.SubmitEnterpriseInquiry)
		}
	}

	// Admin routes (requires admin permissions)
	adminRoutes := r.Group("/admin")
	adminRoutes.Use(m.AuthMiddleware(), m.AdminMiddleware())
	{
		// Settings → Storage backends (pluggable infrastructure registry)
		adminRoutes.GET("/settings/backends", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListStorageBackends)
		adminRoutes.GET("/settings/backends/active/:kind", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminGetActiveStorageBackend)
		adminRoutes.POST("/settings/backends/:id/activate", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminActivateStorageBackend)

		// Cloud providers (Hetzner API token storage)
		adminRoutes.GET("/cloud-credentials", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListCloudCredentials)
		adminRoutes.POST("/cloud-credentials", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminCreateCloudCredential)
		adminRoutes.DELETE("/cloud-credentials/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminDeleteCloudCredential)
		adminRoutes.POST("/cloud-credentials/:id/test", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminTestCloudCredential)

		// Cloud provider catalog (discovery for admin dropdowns)
		adminRoutes.GET("/cloud-providers/:provider/locations", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListProviderLocations)
		adminRoutes.GET("/cloud-providers/:provider/server-types", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListProviderServerTypes)
		adminRoutes.GET("/cloud-providers/:provider/images", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListProviderImages)

		// Provisioning templates (saved configs for one-click provisioning)
		adminRoutes.GET("/provisioning-templates", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListProvisioningTemplates)
		adminRoutes.GET("/provisioning-templates/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminGetProvisioningTemplate)
		adminRoutes.POST("/provisioning-templates", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminCreateProvisioningTemplate)
		adminRoutes.PUT("/provisioning-templates/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminUpdateProvisioningTemplate)
		adminRoutes.DELETE("/provisioning-templates/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminDeleteProvisioningTemplate)

		// Provisioning jobs (state machine + history)
		adminRoutes.GET("/provisioning-jobs", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminListProvisioningJobs)
		adminRoutes.GET("/provisioning-jobs/:id", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminGetProvisioningJob)
		adminRoutes.POST("/provisioning-jobs", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminCreateProvisioningJob)
		adminRoutes.POST("/provisioning-jobs/:id/retry", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminRetryProvisioningJob)

		// Provisioning policy (per-provider budget caps + auto-provision toggle)
		adminRoutes.GET("/provisioning-policy", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListProvisioningPolicy)
		adminRoutes.PUT("/provisioning-policy", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminUpdateProvisioningPolicy)

		// User Management
		adminRoutes.GET("/users", middleware.RequireAdminPermission(models.AdminPermViewUsers), h.AdminSearchUsers)
		adminRoutes.GET("/users/:id", middleware.RequireAdminPermission(models.AdminPermViewUsers), h.AdminGetUser)
		adminRoutes.GET("/users/:id/preview", middleware.RequireAdminPermission(models.AdminPermViewUsers), h.AdminGetUserPreview)
		adminRoutes.POST("/users/:id/ban", middleware.RequireAdminPermission(models.AdminPermBanUsers), h.AdminBanUser)
		adminRoutes.POST("/users/:id/unban", middleware.RequireAdminPermission(models.AdminPermBanUsers), h.AdminUnbanUser)
		adminRoutes.GET("/users/:id/bans", middleware.RequireAdminPermission(models.AdminPermViewUsers), h.AdminGetUserBans)
		adminRoutes.GET("/users/:id/campaigns", middleware.RequireAdminPermission(models.AdminPermViewCampaigns), h.AdminGetUserCampaigns)
		adminRoutes.GET("/users/:id/emails", middleware.RequireAdminPermission(models.AdminPermViewUsers), h.AdminGetUserEmails)
		adminRoutes.GET("/users/:id/rate-limits", middleware.RequireAdminPermission(models.AdminPermManageRateLimits), h.AdminGetUserRateLimits)
		adminRoutes.PATCH("/users/:id/rate-limits", middleware.RequireAdminPermission(models.AdminPermManageRateLimits), h.AdminUpdateUserRateLimits)

		// Organization (Workspace) Management
		adminRoutes.GET("/organizations", middleware.RequireAdminPermission(models.AdminPermViewOrganizations), h.AdminListOrganizations)
		adminRoutes.GET("/organizations/:id", middleware.RequireAdminPermission(models.AdminPermViewOrganizations), h.AdminGetOrganization)
		adminRoutes.GET("/organizations/:id/members", middleware.RequireAdminPermission(models.AdminPermViewOrganizations), h.AdminGetOrganizationMembers)
		adminRoutes.GET("/organizations/:id/overrides", middleware.RequireAdminPermission(models.AdminPermViewOrganizations), h.AdminGetOrgOverrides)
		adminRoutes.PUT("/organizations/:id/overrides", middleware.RequireAdminPermission(models.AdminPermManageOrganizations), h.AdminUpdateOrgOverrides)

		// Limit-increase request queue. Approval writes the override
		// row via the same SetLimitOverrides path used by direct
		// edits, so granted_by + audit-log story stays unified.
		adminRoutes.GET("/limit-requests", middleware.RequireAdminPermission(models.AdminPermViewOrganizations), h.AdminListLimitRequests)
		adminRoutes.POST("/limit-requests/:id/approve", middleware.RequireAdminPermission(models.AdminPermManageOrganizations), h.AdminApproveLimitRequest)
		adminRoutes.POST("/limit-requests/:id/reject", middleware.RequireAdminPermission(models.AdminPermManageOrganizations), h.AdminRejectLimitRequest)

		// Admin outreach composer. Reuses ManageOrganizations (the
		// audit story is the same as direct overrides — admin sends
		// a thing on behalf of the platform); a dedicated
		// SendOutreach bit can be carved out later if outreach review
		// becomes its own surface.
		adminRoutes.POST("/outreach", middleware.RequireAdminPermission(models.AdminPermManageOrganizations), h.AdminSendOutreach)
		adminRoutes.GET("/outreach", middleware.RequireAdminPermission(models.AdminPermViewOrganizations), h.AdminListOutreach)

		// Worker Management
		adminRoutes.GET("/workers", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminListWorkers)
		adminRoutes.GET("/workers/:id", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminGetWorker)
		adminRoutes.PATCH("/workers/:id", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminUpdateWorker)
		adminRoutes.GET("/workers/:id/emails", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminGetWorkerEmails)
		adminRoutes.GET("/workers/:id/stats", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminGetWorkerStats)
		adminRoutes.POST("/workers/:id/reassign", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminReassignEmails)

		// SSH-managed worker lifecycle (admin-driven add / install / restart / logs)
		adminRoutes.GET("/workers/managed", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminListSSHWorkers)
		adminRoutes.POST("/workers", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminCreateWorker)
		adminRoutes.GET("/workers/:id/managed", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminGetSSHWorker)
		adminRoutes.POST("/workers/:id/test", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminTestWorker)
		adminRoutes.POST("/workers/:id/install", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminInstallWorker)
		adminRoutes.POST("/workers/:id/restart", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminRestartWorker)
		adminRoutes.POST("/workers/:id/upgrade", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminUpdateWorkerImage)
		adminRoutes.POST("/workers/:id/uninstall", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminUninstallWorker)
		adminRoutes.POST("/workers/:id/rotate-keys", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminRotateWorkerKeys)
		adminRoutes.GET("/workers/:id/live-status", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminWorkerStatusLive)
		adminRoutes.GET("/workers/:id/logs", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminWorkerLogs)
		adminRoutes.DELETE("/workers/:id", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminDeleteSSHWorker)
		adminRoutes.PUT("/workers/:id/profile", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminAssignWorkerProfile)
		adminRoutes.POST("/workers/:id/apply", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminApplyWorkerConfig)
		adminRoutes.POST("/workers/:id/system-update", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminSystemUpdate)
		adminRoutes.POST("/workers/:id/reboot", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminRebootWorker)
		adminRoutes.POST("/workers/:id/convert-to-dedicated", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminConvertWorkerToDedicated)
		adminRoutes.PUT("/workers/:id/risk-pool", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminSetWorkerRiskPool)
		adminRoutes.POST("/workers/preflight", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminPreflightWorker)
		adminRoutes.GET("/workers/tags", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminListWorkerTags)
		adminRoutes.PUT("/workers/:id/tags", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminSetWorkerTags)

		// Reusable AWS credentials (gated under AdminPermManageSettings — these
		// hold real production secrets, not just worker assignments).
		adminRoutes.GET("/aws-credentials", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListAWSCreds)
		adminRoutes.POST("/aws-credentials", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminCreateAWSCreds)
		adminRoutes.GET("/aws-credentials/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminGetAWSCreds)
		adminRoutes.PATCH("/aws-credentials/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminUpdateAWSCreds)
		adminRoutes.DELETE("/aws-credentials/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminDeleteAWSCreds)

		// Reusable worker profiles
		adminRoutes.GET("/worker-profiles", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminListProfiles)
		adminRoutes.POST("/worker-profiles", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminCreateProfile)
		adminRoutes.GET("/worker-profiles/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminGetProfile)
		adminRoutes.PATCH("/worker-profiles/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminUpdateProfile)
		adminRoutes.DELETE("/worker-profiles/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminDeleteProfile)
		adminRoutes.GET("/worker-profiles/:id/workers", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminListProfileWorkers)
		adminRoutes.POST("/worker-profiles/:id/apply", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminApplyProfile)
		adminRoutes.PUT("/worker-profiles/:id/release", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminSetProfileRelease)

		// Release auto-update: manual trigger + last-known state for the UI.
		adminRoutes.POST("/releases/check", middleware.RequireAdminPermission(models.AdminPermManageWorkers), h.AdminCheckReleases)
		adminRoutes.GET("/releases/state", middleware.RequireAdminPermission(models.AdminPermViewWorkers), h.AdminReleasesState)

		// Warmup Management
		adminRoutes.GET("/warmup/pools", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListWarmupPools)
		adminRoutes.GET("/warmup/health", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminGetWarmupHealthSummary)
		adminRoutes.GET("/warmup/pools/:type/participants", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminGetPoolParticipants)
		adminRoutes.GET("/warmup/blocked", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListBlockedAccounts)
		adminRoutes.POST("/warmup/block/:accountId", middleware.RequireAdminPermission(models.AdminPermManageWarmupBans), h.AdminBlockAccount)
		adminRoutes.POST("/warmup/unblock/:accountId", middleware.RequireAdminPermission(models.AdminPermManageWarmupBans), h.AdminUnblockAccount)

		// Warmup Appeals
		adminRoutes.GET("/warmup/appeals", middleware.RequireAdminPermission(models.AdminPermReviewAppeals), h.AdminListAppeals)
		adminRoutes.GET("/warmup/appeals/:id", middleware.RequireAdminPermission(models.AdminPermReviewAppeals), h.AdminGetAppeal)
		adminRoutes.POST("/warmup/appeals/:id/approve", middleware.RequireAdminPermission(models.AdminPermReviewAppeals), h.AdminApproveAppeal)
		adminRoutes.POST("/warmup/appeals/:id/reject", middleware.RequireAdminPermission(models.AdminPermReviewAppeals), h.AdminRejectAppeal)

		// Warmup content bank + offline AI generator. Reads use the warmup
		// view permission; the A/B analytics uses the analytics permission;
		// mutations (generate, archive/delete, settings) use ManageSettings.
		adminRoutes.GET("/warmup-content/overview", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminWarmupContentOverview)
		adminRoutes.GET("/warmup-content/conversations", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListWarmupConversations)
		adminRoutes.GET("/warmup-content/conversations/:id", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminGetWarmupConversation)
		adminRoutes.POST("/warmup-content/conversations/:id/archive", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminArchiveWarmupConversation)
		adminRoutes.POST("/warmup-content/conversations/:id/unarchive", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminUnarchiveWarmupConversation)
		adminRoutes.DELETE("/warmup-content/conversations/:id", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminDeleteWarmupConversation)
		adminRoutes.POST("/warmup-content/generate", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminGenerateWarmupContent)
		adminRoutes.POST("/warmup-content/batch", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminSubmitWarmupBatch)
		adminRoutes.POST("/warmup-content/jobs/:id/cancel", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminCancelWarmupBatch)
		// Seed inbox-placement testing.
		adminRoutes.GET("/placement/tests", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListPlacementTests)
		adminRoutes.GET("/placement/tests/:id", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminGetPlacementTest)
		adminRoutes.POST("/placement/tests", middleware.RequireAdminPermission(models.AdminPermManageWarmupBans), h.AdminCreatePlacementTest)
		adminRoutes.GET("/placement/seeds", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListSeedMailboxes)
		adminRoutes.GET("/placement/seeds/candidates", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListSeedCandidates)
		adminRoutes.POST("/placement/seeds/:id", middleware.RequireAdminPermission(models.AdminPermManageWarmupBans), h.AdminSetSeedMailbox)

		adminRoutes.GET("/warmup-content/jobs", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListWarmupGenerationJobs)
		adminRoutes.GET("/warmup-content/jobs/:id", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminGetWarmupGenerationJob)
		adminRoutes.GET("/warmup-content/settings", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminGetWarmupGenerationSettings)
		adminRoutes.PUT("/warmup-content/settings", middleware.RequireAdminPermission(models.AdminPermManageSettings), h.AdminUpdateWarmupGenerationSettings)
		adminRoutes.GET("/warmup-content/ab", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminWarmupContentAB)

		// Mailbox admin (cross-org). Reuses ViewUsers since mailboxes
		// are tightly coupled to user/org context; a dedicated bit
		// can be carved later if mailbox-specific actions land.
		adminRoutes.GET("/mailboxes", middleware.RequireAdminPermission(models.AdminPermViewUsers), h.AdminSearchMailboxes)

		// Campaign Management
		adminRoutes.GET("/campaigns", middleware.RequireAdminPermission(models.AdminPermViewCampaigns), h.AdminSearchCampaigns)
		adminRoutes.GET("/campaigns/:id", middleware.RequireAdminPermission(models.AdminPermViewCampaigns), h.AdminGetCampaign)
		adminRoutes.POST("/campaigns/:id/stop", middleware.RequireAdminPermission(models.AdminPermStopCampaigns), h.AdminStopCampaign)

		// Analytics Dashboard
		adminRoutes.GET("/analytics/overview", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminGetPlatformOverview)
		adminRoutes.GET("/analytics/trends", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminGetAnalyticsTrends)
		adminRoutes.GET("/analytics/emails/daily", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminGetDailyEmailStats)
		adminRoutes.GET("/analytics/emails/hourly", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminGetHourlyEmailStats)
		adminRoutes.GET("/analytics/workers/load", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminGetWorkerLoadStats)
		adminRoutes.GET("/analytics/workers/distribution", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminGetEmailDistribution)
		adminRoutes.GET("/analytics/users/growth", middleware.RequireAdminPermission(models.AdminPermViewAnalytics), h.AdminGetUserGrowthStats)

		// Plans Management
		adminRoutes.GET("/plans", middleware.RequireAdminPermission(models.AdminPermManagePlans), h.AdminListPlans)
		adminRoutes.POST("/plans", middleware.RequireAdminPermission(models.AdminPermManagePlans), h.AdminCreatePlan)
		adminRoutes.GET("/plans/:id", middleware.RequireAdminPermission(models.AdminPermManagePlans), h.AdminGetPlan)
		adminRoutes.PATCH("/plans/:id", middleware.RequireAdminPermission(models.AdminPermManagePlans), h.AdminUpdatePlan)
		adminRoutes.DELETE("/plans/:id", middleware.RequireAdminPermission(models.AdminPermManagePlans), h.AdminDeletePlan)

		// Discount / promo codes
		adminRoutes.GET("/discounts", middleware.RequireAdminPermission(models.AdminPermManageBilling), h.AdminListDiscounts)
		adminRoutes.POST("/discounts", middleware.RequireAdminPermission(models.AdminPermManageBilling), h.AdminCreateDiscount)
		adminRoutes.GET("/discounts/:id", middleware.RequireAdminPermission(models.AdminPermManageBilling), h.AdminGetDiscount)
		adminRoutes.PATCH("/discounts/:id", middleware.RequireAdminPermission(models.AdminPermManageBilling), h.AdminUpdateDiscount)
		adminRoutes.DELETE("/discounts/:id", middleware.RequireAdminPermission(models.AdminPermManageBilling), h.AdminDeleteDiscount)
		adminRoutes.GET("/discounts/:id/redemptions", middleware.RequireAdminPermission(models.AdminPermManageBilling), h.AdminListDiscountRedemptions)

		// Enterprise Inquiries
		adminRoutes.GET("/enterprise/inquiries", middleware.RequireAdminPermission(models.AdminPermViewEnterpriseInquiries), h.AdminListEnterpriseInquiries)
		adminRoutes.GET("/enterprise/inquiries/:id", middleware.RequireAdminPermission(models.AdminPermViewEnterpriseInquiries), h.AdminGetEnterpriseInquiry)
		adminRoutes.PATCH("/enterprise/inquiries/:id", middleware.RequireAdminPermission(models.AdminPermManageEnterpriseInquiries), h.AdminUpdateEnterpriseInquiry)

		// Admin Management
		adminRoutes.GET("/admins", middleware.RequireAdminPermission(models.AdminPermGrantAdminAccess), h.AdminListAdmins)
		adminRoutes.POST("/admins/:userId/grant", middleware.RequireAdminPermission(models.AdminPermGrantAdminAccess), h.AdminGrantPermissions)
		adminRoutes.POST("/admins/:userId/revoke", middleware.RequireAdminPermission(models.AdminPermGrantAdminAccess), h.AdminRevokePermissions)

		// Audit Logs
		adminRoutes.GET("/audit-logs", middleware.RequireAdminPermission(models.AdminPermViewAuditLogs), h.AdminSearchAuditLogs)

		// Permission list (for admin UI)
		adminRoutes.GET("/permissions", h.AdminGetPermissionList)
	}

	webhook := r.Group("/webhook")
	webhook.Use(oidcm.Middleware())
	{
		webhook.POST("/campaign", h.HandleCampaignTasks)
		webhook.POST("/email", h.HandleEmailTask)
		webhook.POST("/user-email", h.HandleUserEmailTask)
	}

	// Stripe webhook (no auth - uses signature verification)
	r.POST("/webhook/stripe", h.HandleStripeWebhook)

	return r
}
