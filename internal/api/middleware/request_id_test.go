package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestIDMiddlewareUsesClientRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetString(RequestIDContextKey))
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(RequestIDHeader, "client-trace_123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get(RequestIDHeader); got != "client-trace_123" {
		t.Fatalf("response request id = %q", got)
	}
	if got := rec.Body.String(); got != "client-trace_123" {
		t.Fatalf("context request id = %q", got)
	}
}

func TestRequestIDMiddlewareReplacesUnsafeRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetString(RequestIDContextKey))
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(RequestIDHeader, "bad/request/id")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	got := rec.Header().Get(RequestIDHeader)
	if got == "" || got == "bad/request/id" {
		t.Fatalf("response request id = %q", got)
	}
	if got != rec.Body.String() {
		t.Fatalf("header request id %q does not match context %q", got, rec.Body.String())
	}
}
