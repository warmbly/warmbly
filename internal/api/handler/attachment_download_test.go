package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/attachmentdownload"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type downloadCampaigns struct {
	campaign.CampaignService
	org, id uuid.UUID
}

func (s downloadCampaigns) Get(_ context.Context, org, id string) (*models.Campaign, *errx.Error) {
	if org != s.org.String() || id != s.id.String() {
		return nil, errx.ErrNotFound
	}
	return &models.Campaign{ID: s.id}, nil
}

type downloadAttachments struct {
	repository.AttachmentRepository
	att *models.CampaignAttachment
	err error
}

func (r downloadAttachments) GetByID(context.Context, uuid.UUID) (*models.CampaignAttachment, error) {
	return r.att, r.err
}

func TestCampaignDownloadUsesScopedCapabilityAndSafeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	org, campaignID, attID := uuid.New(), uuid.New(), uuid.New()
	store, err := storage.NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	const body = "<script>window.example = true</script>"
	att := &models.CampaignAttachment{ID: attID, CampaignID: campaignID, S3Key: "attachments/test/file", Filename: "report.html", MimeType: "text/html", Size: int64(len(body))}
	if err := store.Put(context.Background(), att.S3Key, strings.NewReader(body), att.MimeType); err != nil {
		t.Fatal(err)
	}
	signer := attachmentdownload.New("test-only-download-key-with-at-least-32-bytes")
	claims := attachmentdownload.Claims{OrganizationID: org, CampaignID: campaignID, AttachmentID: attID}
	for _, tc := range []struct {
		name, token string
		att         *models.CampaignAttachment
		err         error
		want        int
	}{
		{"owned download", signer.Token(claims, time.Now()), att, nil, http.StatusOK},
		{"invalid capability", "invalid", att, nil, http.StatusNotFound},
		{"expired capability", signer.Token(claims, time.Now().Add(-16*time.Minute)), att, nil, http.StatusNotFound},
		{"foreign tenant", signer.Token(attachmentdownload.Claims{OrganizationID: uuid.New(), CampaignID: campaignID, AttachmentID: attID}, time.Now()), att, nil, http.StatusNotFound},
		{"different campaign", signer.Token(claims, time.Now()), &models.CampaignAttachment{CampaignID: uuid.New()}, nil, http.StatusNotFound},
		{"deleted attachment", signer.Token(claims, time.Now()), nil, nil, http.StatusNotFound},
		{"database failure", signer.Token(claims, time.Now()), nil, errors.New("private database detail"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{Storage: store, AttachmentDownloads: signer, CampaignService: downloadCampaigns{org: org, id: campaignID}, AttachmentRepo: downloadAttachments{att: tc.att, err: tc.err}}
			r := gin.New()
			r.GET("/download/:token", h.DownloadCampaignAttachment)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/download/"+tc.token, nil))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
			if strings.Contains(w.Body.String(), "private database detail") {
				t.Fatal("response exposed a private error")
			}
			if tc.want == http.StatusOK {
				if w.Body.String() != body || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
					t.Fatalf("unexpected download body or headers: %v", w.Header())
				}
			}
		})
	}
	t.Setenv("API_PUBLIC_URL", "https://api.example.test")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(middleware.OrganizationIDKey, org)
	h := &Handler{Storage: store, AttachmentDownloads: signer}
	response := h.attachmentResponse(c, att)
	url, ok := response["url"].(string)
	if !ok || !strings.HasPrefix(url, "https://api.example.test/v1/campaign-attachments/download/") {
		t.Fatalf("download link = %v", response["url"])
	}
}
