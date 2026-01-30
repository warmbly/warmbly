package api

import (
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
) {
	gin.SetMode(ginMode)

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"POST", "GET", "PATCH", "OPTIONS", "DELETE"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	auth := r.Group("/auth")
	{
		r.POST("/login/start", h.LoginStart)
		r.POST("/login/confirm", h.LoginConfirm)
		r.POST("/register/start", h.RegistrationStart)
		r.POST("/register/confirm", h.RegistrationConfirm)
		r.POST("/refresh", h.RefreshToken)
		r.POST("/reset-password/start", h.ResetPasswordStart)
		r.POST("/reset-password/confirm", h.ResetPasswordStart)
	}

	protectedAuth := auth.Group("")
	protectedAuth.Use(m.AuthMiddleware())
	{
		r.POST("/logout", h.Logout)
		r.POST("/logout-all", h.LogoutAll)
		r.GET("/me", h.GetUser)
	}

	protected := r.Group("")
	protected.Use(m.AuthMiddleware())
	{
		emails := protected.Group("/emails")
		{
			emails.GET("", h.EmailsSearch)
			emails.GET("/:id", h.GetEmail)
			emails.PATCH("/:id", h.UpdateEmail)
			emails.PATCH("/:id/track", h.UpdateEmailTrackingDomain)
			emails.DELETE("/:id", h.DeleteEmail)
			emails.POST("/:id/send", m.RequireOrganization(), h.SendEmailFromAccount)
		}

		campaigns := protected.Group("/campaigns")
		{
			campaigns.GET("", h.SearchCampaigns)
			campaigns.POST("", h.CreateCampaign)
			campaigns.GET("/:id", h.GetCampaign)
			campaigns.PATCH("/:id", h.UpdateCampaign)
			campaigns.DELETE("/:id", h.DeleteCampaign)

			// Campaign start/stop
			campaigns.POST("/:id/start", m.RequireOrganization(), m.RequirePermission(models.PermSendCampaigns), h.StartCampaign)
			campaigns.POST("/:id/stop", m.RequireOrganization(), m.RequirePermission(models.PermSendCampaigns), h.StopCampaign)
			campaigns.GET("/:id/logs", h.GetCampaignLogs)

			sequences := campaigns.Group("/:id/sequences")
			{
				sequences.GET("", h.GetSequences)
				sequences.POST("", h.CreateSequence)
				sequences.PATCH("/:sid", h.UpdateSequence)
				sequences.DELETE("/:sid", h.DeleteSequence)
			}
		}

		contacts := protected.Group("/contacts")
		{
			contacts.POST("/search", h.SearchContacts)
			contacts.POST("", h.AddContacts)
			contacts.DELETE("", h.DeleteContactBulk)
			contacts.PATCH("", h.UpdateContactBulk)
			contacts.PATCH("/:id", h.UpdateContact)
			contacts.DELETE("/:id", h.DeleteContact)

			// CRM: Notes & Activities (under contacts)
			contacts.GET("/:id/notes", h.ListContactNotes)
			contacts.POST("/:id/notes", h.CreateContactNote)
			contacts.PATCH("/:id/notes/:noteId", h.UpdateContactNote)
			contacts.DELETE("/:id/notes/:noteId", h.DeleteContactNote)
			contacts.GET("/:id/activities", h.ListContactActivities)
			contacts.GET("/:id/deals", h.GetDealsByContact)
		}

		grouph.New(protected, h.FolderService, "folders")
		grouph.New(protected, h.TagService, "tags")
		grouph.New(protected, h.CategoryService, "categories")

		unibox := protected.Group("/unibox")
		{
			unibox.GET("", h.GetUniboxIncoming)
			unibox.GET("/count", h.GetUnseenCount)
			unibox.GET("/thread", h.GetUniboxThread)
			unibox.PATCH("/seen", h.UniboxMarkSeen)
			unibox.GET("/:id", h.GetUniboxEmail)
		}

		protected.POST("/getaway", h.GenerateWebsocket)

		// API Keys management (org-scoped)
		apiKeys := protected.Group("/api-keys")
		apiKeys.Use(m.RequireOrganization(), m.RequirePermission(models.PermManageAPIKeys))
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
		analytics.Use(m.RateLimitMiddleware(models.RateLimitAnalytics))
		{
			// Dashboard overview
			analytics.GET("/dashboard", h.GetDashboardAnalytics)

			// Warmup analytics
			analytics.GET("/warmup", h.GetWarmupAnalytics)

			// Campaign analytics
			analytics.GET("/campaigns/compare", h.CompareCampaigns)
			analytics.GET("/campaigns/:id", h.GetCampaignAnalytics)
			analytics.GET("/campaigns/:id/daily", h.GetCampaignDailyStats)
			analytics.GET("/campaigns/:id/hourly", h.GetCampaignHourlyStats)

			// Account analytics
			analytics.GET("/accounts", h.GetAllAccountStatuses)
			analytics.GET("/accounts/:id", h.GetAccountStatus)

			// Usage overview
			analytics.GET("/usage", h.GetUsageOverview)
		}

		// Audit logs
		auditLogs := protected.Group("/audit-logs")
		auditLogs.Use(m.RateLimitMiddleware(models.RateLimitRead))
		{
			auditLogs.GET("", h.GetAuditLogs)
		}

		// Realtime subscription info
		realtime := protected.Group("/realtime")
		{
			realtime.GET("/info", h.GetRealtimeInfo)
		}

		// Organization management
		org := protected.Group("/organization")
		{
			org.POST("", h.CreateOrganization)
			org.GET("", h.GetUserOrganizations)
			org.POST("/switch/:id", h.SwitchOrganization)

			// Current organization operations (require organization selected)
			org.GET("/current", h.GetCurrentOrganization)
			org.PATCH("/current", m.RequireOrganization(), m.RequirePermission(models.PermManageSettings), h.UpdateOrganization)
			org.GET("/current/limits", m.RequireOrganization(), h.GetOrganizationLimits)

			// Member management
			org.GET("/members", m.RequireOrganization(), h.GetMembers)
			org.POST("/members/invite", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.InviteMember)
			org.PATCH("/members/:id", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.UpdateMemberRole)
			org.DELETE("/members/:id", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.RemoveMember)

			// Invitations
			org.GET("/invitations", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.GetPendingInvitations)
			org.DELETE("/invitations/:id", m.RequireOrganization(), m.RequirePermission(models.PermManageTeam), h.CancelInvitation)

			// Ownership transfer
			org.POST("/transfer-ownership", m.RequireOrganization(), m.RequirePermission(models.PermTransferOwnership), h.TransferOwnership)
		}

		// User's pending invitations
		protected.GET("/invitations", h.GetMyPendingInvitations)
		protected.POST("/invitations/accept", h.AcceptInvitation)

		// Subscription & billing
		subscriptions := protected.Group("/subscription")
		{
			subscriptions.GET("", h.GetSubscription)
			subscriptions.GET("/limits", h.GetSubscriptionLimits)
			subscriptions.GET("/trial", h.GetTrialStatus)
			subscriptions.GET("/features", h.GetFeatureStatus)
			subscriptions.POST("/checkout", h.CreateCheckoutSession)
			subscriptions.POST("/portal", h.CreateBillingPortalSession)
			subscriptions.POST("/cancel", h.CancelSubscription)

			// Plan changes with proration
			subscriptions.POST("/change-plan", m.RequireOrganization(), m.RequirePermission(models.PermManageBilling), h.ChangePlan)
			subscriptions.GET("/preview-change", m.RequireOrganization(), m.RequirePermission(models.PermManageBilling), h.PreviewPlanChange)

			// Enterprise inquiry (no permission required)
			subscriptions.POST("/enterprise-inquiry", h.SubmitEnterpriseInquiry)
		}

		// Plans (public info but auth required for consistency)
		protected.GET("/plans", h.ListPlans)

		// Reply templates (org-scoped)
		templates := protected.Group("/templates")
		templates.Use(m.RequireOrganization())
		{
			templates.GET("", h.ListTemplates)
			templates.POST("", h.CreateTemplate)
			templates.GET("/:id", h.GetTemplate)
			templates.PATCH("/:id", h.UpdateTemplate)
			templates.DELETE("/:id", h.DeleteTemplate)
		}

		// CRM routes (require org)
		crmGroup := protected.Group("/crm")
		crmGroup.Use(m.RequireOrganization())
		{
			// Pipelines
			pipelines := crmGroup.Group("/pipelines")
			{
				pipelines.GET("", h.ListPipelines)
				pipelines.POST("", h.CreatePipeline)
				pipelines.GET("/:id", h.GetPipeline)
				pipelines.PATCH("/:id", h.UpdatePipeline)
				pipelines.DELETE("/:id", h.DeletePipeline)
				pipelines.POST("/:id/stages", h.CreateStage)
				pipelines.PATCH("/:id/stages/:stageId", h.UpdateStage)
				pipelines.DELETE("/:id/stages/:stageId", h.DeleteStage)
			}

			// Deals
			deals := crmGroup.Group("/deals")
			{
				deals.GET("", h.ListDeals)
				deals.POST("", h.CreateDeal)
				deals.GET("/:id", h.GetDeal)
				deals.PATCH("/:id", h.UpdateDeal)
				deals.DELETE("/:id", h.DeleteDeal)
			}

			// CRM Tasks
			crmTasks := crmGroup.Group("/tasks")
			{
				crmTasks.GET("", h.ListCRMTasks)
				crmTasks.POST("", h.CreateCRMTask)
				crmTasks.GET("/:id", h.GetCRMTask)
				crmTasks.PATCH("/:id", h.UpdateCRMTask)
				crmTasks.DELETE("/:id", h.DeleteCRMTask)
			}
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

		// Warmup Management
		adminRoutes.GET("/warmup/pools", middleware.RequireAdminPermission(models.AdminPermViewWarmupPool), h.AdminListWarmupPools)
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

	r.Run(addr)
}
