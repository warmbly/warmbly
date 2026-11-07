package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/config"
)

func TestDashboardOriginMiddleware(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	r := gin.New()
	r.Use(DashboardOriginMiddleware())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, config.DashboardBaseURL(c.Request.Context())) })
	for _, tt := range []struct{ origin, want string }{
		{"https://second.example.com", "https://second.example.com"},
		{"https://evil.example.com", "https://app.warmbly.com"},
		{"", "https://app.warmbly.com"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", tt.origin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Body.String() != tt.want {
			t.Errorf("request origin %q returned %q", tt.origin, w.Body.String())
		}
	}
}
