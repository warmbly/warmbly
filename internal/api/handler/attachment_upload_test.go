package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type uploadQuota struct{ feature.FeatureGateService }

func (uploadQuota) GetStorageLimitBytes(context.Context, uuid.UUID) (int64, *errx.Error) {
	return 1 << 30, nil
}

type uploadAudit struct{ audit.AuditService }

func (uploadAudit) LogAction(context.Context, uuid.UUID, uuid.UUID, models.AuditAction, models.AuditEntityType, *uuid.UUID, string, string, map[string]string, map[string]string) {
}

type uploadAttachments struct {
	repository.AttachmentRepository
	row *models.CampaignAttachment
}

func (r *uploadAttachments) SumStorageUsedByOrg(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *uploadAttachments) CreateWithinQuota(ctx context.Context, a *models.CampaignAttachment, org uuid.UUID, limit repository.StorageLimitFunc) (bool, int64, int64, error) {
	r.row = a
	a.ID = uuid.New()
	l, e := limit(ctx)
	return true, 0, l, e
}

type uploadStore struct {
	storage.Store
	puts int
}

func (s *uploadStore) Put(ctx context.Context, key string, body io.Reader, ct string) error {
	s.puts++
	return s.Store.Put(ctx, key, body, ct)
}

func TestAttachmentUploadUsesContentPolicyBeforeStorageAndReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	org, campaign, user := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name, filename, clientMIME string
		body                       []byte
		want                       int
		canonicalMIME              string
	}{
		{"generic client type", "people.csv", "application/octet-stream", []byte("name;city\nRenée;東京\n"), http.StatusCreated, "text/csv"},
		{"forged client type", "people.csv", "text/html", []byte("name,city\nRenée,東京\n"), http.StatusCreated, "text/csv"},
		{"Unicode text", "notes.txt", "application/octet-stream", []byte("Notes for Renée in 東京.\n"), http.StatusCreated, "text/plain"},
		{"CSV field boundary", "data.csv", "text/html", []byte("\ufeff" + strings.Repeat("x;", 4095) + "\r\n"), http.StatusCreated, "text/csv"},
		{"TSV field boundary", "data.tsv", "application/octet-stream", []byte(strings.Repeat("x\t", 4095) + "\n"), http.StatusCreated, "text/tab-separated-values"},
		{"CSV record field limit", "data.csv", "text/csv", []byte(strings.Repeat("x,", 4096) + "\n"), http.StatusBadRequest, ""},
		{"TSV record field limit", "data.tsv", "text/tab-separated-values", []byte(strings.Repeat("x\t", 4096) + "\n"), http.StatusBadRequest, ""},
		{"renamed binary", "document.pdf", "application/pdf", []byte("MZ\x00binary"), http.StatusBadRequest, ""},
		{"unknown", "file.unknown", "text/plain", []byte("ordinary text"), http.StatusBadRequest, ""},
		{"invalid container", "document.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK\x03\x04malformed"), http.StatusBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := storage.NewFilesystem(t.TempDir(), "")
			if err != nil {
				t.Fatal(err)
			}
			store := &uploadStore{Store: files}
			repo := &uploadAttachments{}
			h := &Handler{Storage: store, CampaignService: downloadCampaigns{org: org, id: campaign}, AttachmentRepo: repo, FeatureGateService: uploadQuota{}, AuditService: uploadAudit{}}
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			headers := textproto.MIMEHeader{}
			headers.Set("Content-Disposition", `form-data; name="file"; filename="`+tc.filename+`"`)
			headers.Set("Content-Type", tc.clientMIME)
			part, err := form.CreatePart(headers)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(tc.body); err != nil {
				t.Fatal(err)
			}
			if err := form.Close(); err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/campaigns/:id/attachments", func(c *gin.Context) {
				c.Set(middleware.OrganizationIDKey, org)
				c.Set(middleware.UserIDKey, user.String())
			}, h.UploadCampaignAttachment)
			req := httptest.NewRequest(http.MethodPost, "/campaigns/"+campaign.String()+"/attachments", &body)
			req.Header.Set("Content-Type", form.FormDataContentType())
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.want == http.StatusCreated {
				if repo.row == nil || repo.row.MimeType != tc.canonicalMIME || repo.row.Size != int64(len(tc.body)) {
					t.Fatalf("canonical metadata=%+v", repo.row)
				}
				r, err := files.Get(t.Context(), repo.row.S3Key)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				got, err := io.ReadAll(r)
				if err != nil || !bytes.Equal(got, tc.body) {
					t.Fatal("bytes changed")
				}
			} else if store.puts != 0 || repo.row != nil || !strings.Contains(w.Body.String(), `"code":"invalid_attachment"`) {
				t.Fatal("refused content was stored/reserved or lacks stable code", w.Body.String())
			}
		})
	}
}
