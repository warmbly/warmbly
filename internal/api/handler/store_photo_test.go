package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/models"
)

func storePhotoRouter(org, user uuid.UUID) *gin.Engine {
	h := &Handler{}
	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), func(c *gin.Context) {
		c.Set(middleware.AuthTypeKey, middleware.AuthTypeJWT)
		c.Set(middleware.UserIDKey, user.String())
		c.Set(middleware.OrganizationIDKey, org)
		c.Next()
	})
	r.POST("/store/photos", h.UploadStorePhoto)
	r.PUT("/store/mailboxes/:id/photo", h.SetStoreMailboxPhoto)
	return r
}

func storeCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}

func TestStoreMailboxPhotoRouteNeedsAnUploadedPhoto(t *testing.T) {
	r := storePhotoRouter(uuid.New(), uuid.New())
	for _, tt := range []struct {
		name, body, want string
	}{
		{"no photo", `{}`, "invalid_body"},
		{"not a photo id", `{"photo_id":"nope"}`, "invalid_body"},
		// Past validation the route reaches the store, which this instance does not have.
		{"a photo id", `{"photo_id":"` + uuid.NewString() + `"}`, "store_disabled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/store/mailboxes/"+uuid.NewString()+"/photo", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if got := storeCode(t, w); got != tt.want {
				t.Fatalf("code = %q (status %d, %s), want %q", got, w.Code, w.Body.String(), tt.want)
			}
		})
	}
}

func TestStorePhotoUploadRefusesWhatIsNotAPhoto(t *testing.T) {
	r := storePhotoRouter(uuid.New(), uuid.New())

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "photo.png")
	_, _ = part.Write([]byte("<svg xmlns='http://www.w3.org/2000/svg' onload='alert(1)'/>"))
	_ = mw.Close()
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/store/photos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.HasPrefix(storeCode(t, w), "image_") {
		t.Fatalf("an SVG upload: status %d, %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/store/photos", strings.NewReader(`{"from":"elsewhere"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if got := storeCode(t, w); got != "invalid_body" {
		t.Fatalf("an unknown source: %q (%s)", got, w.Body.String())
	}
}

// Without a workspace the photo routes answer nothing about any photo.
func TestStorePhotoRoutesNeedAWorkspace(t *testing.T) {
	h := &Handler{}
	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), func(c *gin.Context) {
		c.Set(middleware.UserIDKey, uuid.NewString())
		c.Set(middleware.SessionMemberKey, &models.OrganizationMember{})
		c.Next()
	})
	r.GET("/store/photos/:id", h.GetStorePhoto)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/store/photos/"+uuid.NewString(), nil))
	if w.Code == http.StatusOK {
		t.Fatalf("read a photo with no workspace: %d", w.Code)
	}
}
