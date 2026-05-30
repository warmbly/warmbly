package errx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestJSONIncludesStableCodeAndRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("request_id", "req_test_123")

	JSON(c, New(BadRequest, "invalid cursor"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "bad_request" {
		t.Fatalf("code = %q", body.Code)
	}
	if body.RequestID != "req_test_123" {
		t.Fatalf("request_id = %q", body.RequestID)
	}
	if body.Error != "Bad Request" || body.Message != "invalid cursor" {
		t.Fatalf("unexpected body: %+v", body)
	}
}
