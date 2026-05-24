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

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Public webhook for GitHub release events. Auth comes from
	// X-Hub-Signature-256 (HMAC-SHA256 with RELEASES_WEBHOOK_SECRET).
	r.POST("/webhooks/github/releases", h.GithubReleasesWebhook)

	// Public OAuth-bouncer pages used by the mailbox onboarding popup.
	// The provider redirects here; the page postMessages the code/state
	// back to the SPA opener which then calls /emails/onboarding/oauth/finish.
	r.GET("/addresses/google/callback", h.EmailOAuthCallbackGmail)
	r.GET("/addresses/outlook/callback", h.EmailOAuthCallbackOutlook)

	corsConfig := cors.Config{
		AllowMethods:  []string{"POST", "GET", "PATCH", "OPTIONS", "DELETE"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders: []string{"Content-Length"},
		MaxAge:        12 * time.Hour,
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
		}
		corsConfig.AllowCredentials = true
	case len(allowedOrigins) == 1 && allowedOrigins[0] == "*":
		corsConfig.AllowAllOrigins = true
		corsConfig.AllowCredentials = false
	default:
		corsConfig.AllowOrigins = allowedOrigins
		corsConfig.AllowCredentials = true
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
	}

	protectedAuth := auth.Group("")
	protectedAuth.Use(m.AuthMiddleware())
	{
		protectedAuth.POST("/logout", h.Logout)
		protectedAuth.POST("/logout-all", h.LogoutAll)
		protectedAuth.GET("/me", h.GetUser)
		protectedAuth.PATCH("/me/onboarding", h.CompleteOnboarding)
		protectedAuth.POST("/me/avatar", h.UploadUserAvatar)
		protectedAuth.DELETE("/me/avatar", h.DeleteUserAvatar)
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
	protected.Use(m.CombinedAuthMiddleware(), m.APIKeyUsageMiddleware())
	{
		emails := protected.Group("/emails")
		emails.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			emails.GET("", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadEmails), h.EmailsSearch)
			emails.GET("/:id", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadEmails), h.GetEmail)
			emails.PATCH("/:id", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), h.UpdateEmail)
			emails.PATCH("/:id/track", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), h.UpdateEmailTrackingDomain)
			emails.DELETE("/:id", m.RequireAccess(models.PermManageEmails, models.APIPermWriteEmails), h.DeleteEmail)
			emails.POST("/:id/send", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.SendEmailFromAccount)
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
			campaigns.POST("/:id/preflight", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.RunCampaignPreflight)
			campaigns.GET("/:id/ab-analysis", m.RequireOrganization(), m.RequireAccess(models.PermViewAnalytics, models.APIPermReadAnalytics), h.GetCampaignABAnalysis)
			campaigns.POST("/:id/test-email", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.SendTestEmail)

			// Campaign start/stop
			campaigns.POST("/:id/start", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.StartCampaign)
			campaigns.POST("/:id/stop", m.RequireOrganization(), m.RequireAccess(models.PermSendCampaigns, models.APIPermSendCampaigns), h.StopCampaign)
			campaigns.GET("/:id/logs", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.GetCampaignLogs)

			sequences := campaigns.Group("/:id/sequences")
			{
				sequences.GET("", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.GetSequences)
				sequences.POST("", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.CreateSequence)
				sequences.PATCH("/:sid", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.UpdateSequence)
				sequences.DELETE("/:sid", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.DeleteSequence)
			}
		}

		contacts := protected.Group("/contacts")
		contacts.Use(m.RateLimitMiddleware(models.RateLimitWrite))
		{
			contacts.POST("/search", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.SearchContacts)
			contacts.POST("", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.AddContacts)
			contacts.DELETE("", m.RequireAccess(models.PermManageContacts, models.APIPermBulkContacts), h.DeleteContactBulk)
			contacts.PATCH("", m.RequireAccess(models.PermManageContacts, models.APIPermBulkContacts), h.UpdateContactBulk)
			contacts.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.UpdateContact)
			contacts.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.DeleteContact)

			// CRM: Notes & Activities (under contacts)
			contacts.GET("/:id/notes", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ListContactNotes)
			contacts.POST("/:id/notes", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.CreateContactNote)
			contacts.PATCH("/:id/notes/:noteId", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.UpdateContactNote)
			contacts.DELETE("/:id/notes/:noteId", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts), h.DeleteContactNote)
			contacts.GET("/:id/activities", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ListContactActivities)
			contacts.GET("/:id/deals", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetDealsByContact)
		}

		// Group endpoints (folders / tags / categories) don't yet have
		// dedicated permission bits — gate them on the broadest read scope
		// for now so an API key needs at least one collection permission.
		grouph.New(protected, h.FolderService, "folders")
		grouph.New(protected, h.TagService, "tags")
		grouph.New(protected, h.CategoryService, "categories")

		unibox := protected.Group("/unibox")
		unibox.Use(m.RateLimitMiddleware(models.RateLimitRead))
		{
			unibox.GET("", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUniboxIncoming)
			unibox.GET("/count", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUnseenCount)
			unibox.GET("/thread", m.RequireAccess(models.PermAccessUnibox, models.APIPermReadUnibox), h.GetUniboxThread)
			unibox.PATCH("/seen", m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.UniboxMarkSeen)
			unibox.POST("/reply", m.RequireOrganization(), m.RequireAccess(models.PermAccessUnibox, models.APIPermWriteUnibox), h.UniboxReply)
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
			apiKeys.GET("/:id", h.GetAPIKey)
			apiKeys.PATCH("/:id", h.UpdateAPIKey)
			apiKeys.DELETE("/:id", h.RevokeAPIKey)
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

		// Reply templates (org-scoped)
		templates := protected.Group("/templates")
		templates.Use(m.RequireOrganization(), m.RateLimitMiddleware(models.RateLimitWrite))
		{
			templates.GET("", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadTemplates), h.ListTemplates)
			templates.POST("", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.CreateTemplate)
			templates.GET("/:id", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadTemplates), h.GetTemplate)
			templates.PATCH("/:id", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.UpdateTemplate)
			templates.DELETE("/:id", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteTemplates), h.DeleteTemplate)
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
				deals.GET("/:id", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetDeal)
				deals.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.UpdateDeal)
				deals.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.DeleteDeal)
			}

			crmTasks := crmGroup.Group("/tasks")
			{
				crmTasks.GET("", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.ListCRMTasks)
				crmTasks.POST("", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.CreateCRMTask)
				crmTasks.GET("/:id", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetCRMTask)
				crmTasks.PATCH("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.UpdateCRMTask)
				crmTasks.DELETE("/:id", m.RequireAccess(models.PermManageContacts, models.APIPermWriteCRM), h.DeleteCRMTask)
			}
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
		}

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
