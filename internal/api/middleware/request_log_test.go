package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLoggerKeepsCredentialsOutOfTheLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	prev := gin.DefaultWriter
	gin.DefaultWriter = &buf
	t.Cleanup(func() { gin.DefaultWriter = prev })

	r := gin.New()
	r.Use(RequestLogger())
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	r.POST("/api/v1/integrations/inbound/calendly/:secret", ok)
	r.POST("/unsubscribe/:token/resubscribe", ok)
	r.GET("/mailboxes/:remoteId/warmup-tokens/:token", ok)
	r.GET("/invitations/lookup", ok)
	r.POST("/cli-auth/codes/:code/approve", ok)
	r.GET("/v1/campaign-attachments/download/:token", ok)

	cases := []struct{ method, target, want, leak string }{
		{"POST", "/api/v1/integrations/inbound/calendly/whsec-abc", "/api/v1/integrations/inbound/calendly/:secret", "whsec-abc"},
		{"POST", "/unsubscribe/unsub-tok/resubscribe", "/unsubscribe/:token/resubscribe", "unsub-tok"},
		{"GET", "/mailboxes/remote-7/warmup-tokens/warm-tok", "/mailboxes/remote-7/warmup-tokens/:token", "warm-tok"},
		{"GET", "/invitations/lookup?token=invite-tok", "/invitations/lookup", "invite-tok"},
		{"POST", "/cli-auth/codes/PAIR1234/approve", "/cli-auth/codes/:code/approve", "PAIR1234"},
		{"GET", "/v1/campaign-attachments/download/private-file-capability", "/v1/campaign-attachments/download/:token", "private-file-capability"},
	}
	for _, tc := range cases {
		buf.Reset()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.target, nil))
		line := buf.String()
		if strings.Contains(line, tc.leak) {
			t.Errorf("%s logged the credential: %q", tc.target, line)
		}
		if !strings.Contains(line, `"`+tc.want+`"`) {
			t.Errorf("%s logged %q, want path %q", tc.target, line, tc.want)
		}
	}
}

func TestRequestLoggerReleaseRetainsFailuresAndMutations(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	var buf bytes.Buffer
	previousWriter := gin.DefaultWriter
	gin.DefaultWriter = &buf
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })

	cases := []struct {
		name, method, path string
		status             int
		log                bool
		handlerError       bool
	}{
		{"healthy probe", "GET", "/health", 200, false, false},
		{"failed probe", "GET", "/health", 503, true, false},
		{"successful internal lookup", "GET", "/api/v1/internal/sync/folder-messages", 200, false, false},
		{"expected missing mapping", "GET", "/api/v1/internal/email-message-map", 404, false, false},
		{"failed mapping lookup", "GET", "/api/v1/internal/email-message-map", 500, true, false},
		{"bad lookup input", "GET", "/api/v1/internal/email-message-map", 400, true, false},
		{"unauthorized lookup", "GET", "/api/v1/internal/email-message-map", 401, true, false},
		{"forbidden lookup", "GET", "/api/v1/internal/email-message-map", 403, true, false},
		{"other missing internal resource", "GET", "/api/v1/internal/sync/folder-messages", 404, true, false},
		{"internal mutation", "PUT", "/api/v1/internal/email-message-map", 204, true, false},
		{"ordinary successful request", "GET", "/v1/emails", 200, true, false},
		{"ordinary missing resource", "GET", "/v1/emails", 404, true, false},
		{"internal handler error with successful status", "GET", "/api/v1/internal/sync/folder-messages", 200, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			r := gin.New()
			r.Use(RequestLogger())
			r.Handle(tc.method, tc.path, func(c *gin.Context) {
				if tc.handlerError {
					_ = c.Error(http.ErrHandlerTimeout)
				}
				c.Status(tc.status)
			})
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil))
			if logged := buf.Len() > 0; logged != tc.log {
				t.Fatalf("logged = %v, want %v", logged, tc.log)
			}
		})
	}
	buf.Reset()
	r := gin.New()
	r.Use(RequestLogger())
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/internal/unknown", nil))
	if buf.Len() == 0 {
		t.Fatal("unmatched internal route was not logged")
	}
}

func TestRequestLoggerDebugKeepsRoutineLookups(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	var buf bytes.Buffer
	previousWriter := gin.DefaultWriter
	gin.DefaultWriter = &buf
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/api/v1/internal/email-message-map", func(c *gin.Context) { c.Status(404) })
	buf.Reset()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/internal/email-message-map", nil))
	if buf.Len() == 0 {
		t.Fatal("debug mode did not log routine lookup")
	}
}
