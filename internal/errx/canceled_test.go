package errx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// A request the caller abandoned mid-query is not answered or logged as a server error.
func TestJSONAnswersAnAbandonedRequestAsClientClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/store/status", nil).WithContext(ctx)

	JSON(c, New(Internal, "failed to get organization"))

	if rec.Code != StatusClientClosedRequest || rec.Body.Len() != 0 {
		t.Fatalf("status %d body %q, want 499 and no body", rec.Code, rec.Body.String())
	}

	live := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(live)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/store/status", nil)
	JSON(c, New(Internal, "failed to get organization"))
	if live.Code != http.StatusInternalServerError {
		t.Fatalf("a live request's server error answered %d", live.Code)
	}
}
