package api

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/api/handler/grouph"
	"github.com/warmbly/warmbly/internal/api/middleware"
)

func Run(h *handler.Handler, m *middleware.Handler, addr, ginMode string) {
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
			unibox.PATCH("/seen", h.UniboxMarkSeen)
			unibox.GET("/:id", h.GetUniboxEmail)
			unibox.GET("/thread", h.GetUniboxThread)
		}

		protected.POST("/getaway", h.GenerateWebsocket)
	}

	r.Run(addr)
}
