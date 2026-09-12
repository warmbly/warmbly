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

// A Code outside the table used to map to status 0, which gin leaves at 200.
// Every error carrying an upstream status (the Warmbly Cloud client is the one
// that does) would then be reported to the caller as a success.
func TestJSONAnswersAnUnknownCodeAsAnError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	JSON(c, New(Code(599), "upstream fell over"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "internal_error" {
		t.Fatalf("code = %q", body.Code)
	}
}
