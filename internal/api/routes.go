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
		}

		campaigns := protected.Group("/campaigns")
		{
			campaigns.GET("", h.SearchCampaigns)
			campaigns.POST("", h.CreateCampaign)
			campaigns.GET("/:id", h.GetCampaign)
			campaigns.PATCH("/:id", h.UpdateCampaign)
			campaigns.DELETE("/:id", h.DeleteCampaign)

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

		// API Keys management
		apiKeys := protected.Group("/api-keys")
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
			analytics.GET("/warmup", h.GetWarmupAnalytics)
			analytics.GET("/campaigns/:id", h.GetCampaignAnalytics)
			analytics.GET("/campaigns/:id/daily", h.GetCampaignDailyStats)
			analytics.GET("/accounts", h.GetAllAccountStatuses)
			analytics.GET("/accounts/:id", h.GetAccountStatus)
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
	}

	// Admin routes (requires additional role check)
	admin := r.Group("/admin")
	admin.Use(m.AuthMiddleware())
	{
		admin.GET("/users/:id/rate-limits", h.GetUserRateLimits)
		admin.PATCH("/users/:id/rate-limits", h.UpdateUserRateLimits)
		admin.GET("/audit-logs", h.GetAdminAuditLogs)
	}

	webhook := r.Group("/webhook")
	webhook.Use(oidcm.Middleware())
	{
		webhook.POST("/campaign", h.HandleCampaignTasks)
		webhook.POST("/email", h.HandleEmailTask)
	}

	// Stripe webhook (no auth - uses signature verification)
	r.POST("/webhook/stripe", h.HandleStripeWebhook)

	r.Run(addr)
}
