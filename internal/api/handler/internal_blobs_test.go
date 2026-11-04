package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/infrastructure/storage"
)

// The presign broker signs only node prefixes, refuses traversal, and logs every refusal.
func TestPresignRefusesForeignPrefixAndLogsIt(t *testing.T) {
	var buf bytes.Buffer
	prev := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	defer func() { zlog.Logger = prev }()

	fs, err := storage.NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Storage: fs}
	r := gin.New()
	r.POST("/presign", h.InternalPresignBlob)
	refused := []string{"avatars/users/a.png", "exports/org.zip", "emails/../avatars/x", "users/../../form-assets/x"}
	for _, key := range refused {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/presign", strings.NewReader(`{"op":"get","key":"`+key+`"}`)))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", key, w.Code)
		}
	}
	out := buf.String()
	if strings.Count(out, `"event":"broker_access"`) != len(refused) || strings.Count(out, `"allowed":false`) != len(refused) {
		t.Fatalf("refusals not logged: %s", out)
	}

	// A node prefix passes the gate; the filesystem backend then declines to sign.
	buf.Reset()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/presign", strings.NewReader(`{"op":"get","key":"emails/abc"}`)))
	if w.Code != http.StatusNotImplemented || !strings.Contains(buf.String(), `"allowed":true`) {
		t.Fatalf("node prefix: status %d, log %s", w.Code, buf.String())
	}
}

type compatiblePresigner struct {
	storage.Store
	calls []presignCall
}

type presignCall struct {
	op               storage.PresignOp
	key, contentType string
}

func (s *compatiblePresigner) PresignedURL(_ context.Context, op storage.PresignOp, key, contentType string, _ time.Duration) (string, error) {
	s.calls = append(s.calls, presignCall{op, key, contentType})
	return "https://storage.example.test/scoped-capability", nil
}

func TestExistingNodeAttachmentCapabilitiesRemainTransportOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &compatiblePresigner{}
	h := &Handler{Storage: store}
	router := gin.New()
	router.POST("/presign", h.InternalPresignBlob)
	for i, tc := range []struct{ op, method string }{{"get", http.MethodGet}, {"head", http.MethodHead}, {"put", http.MethodPut}, {"delete", http.MethodDelete}} {
		request := `{"op":"` + tc.op + `","key":"attachments/existing/file.unknown","content_type":"application/x-existing"}`
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/presign", strings.NewReader(request)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.op, w.Code, w.Body.String())
		}
		var response blobPresignResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Method != tc.method {
			t.Fatalf("%s: response=%+v err=%v", tc.op, response, err)
		}
		want := presignCall{storage.PresignOp(tc.op), "attachments/existing/file.unknown", "application/x-existing"}
		if len(store.calls) != i+1 || store.calls[i] != want {
			t.Fatalf("%s: forwarded=%+v want=%+v", tc.op, store.calls, want)
		}
	}
}
