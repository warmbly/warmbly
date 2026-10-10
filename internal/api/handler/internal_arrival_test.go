package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/repository"
)

type internalArrivalMap struct {
	calls, legacyCalls int
	err                error
}

func TestInternalArrivalLogsOnlySafeDiagnosticsWithoutAcknowledging(t *testing.T) {
	var output bytes.Buffer
	original := log.Logger
	log.Logger = zerolog.New(&output)
	t.Cleanup(func() { log.Logger = original })
	private := "private-provider-payload-and-key"
	m := &internalArrivalMap{err: &pgconn.PgError{Code: "22021", Message: private, Detail: private}}
	h := &Handler{EmailMessageMap: m}
	r := gin.New()
	requestID, user, email, arrival := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	r.Use(func(c *gin.Context) { c.Set(middleware.RequestIDContextKey, requestID) })
	r.POST("/arrival", h.InternalAdmitEmailArrival)
	body := fmt.Sprintf(`{"map":{"user_id":%q,"email_id":%q,"id":%q,"message_id":%q,"thread_id":%q},"pending":{}}`, user, email, arrival, private, private)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/arrival", strings.NewReader(body)))
	if w.Code != 503 || w.Body.Len() != 0 || w.Header().Get("X-Warmbly-Arrival-Durable") != "" || strings.Contains(output.String(), private) {
		t.Fatal("failed admission was acknowledged or exposed private data")
	}
	var diagnostic map[string]any
	if err := json.Unmarshal(output.Bytes(), &diagnostic); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"admission_stage": "dependency", "sqlstate": "22021", "user_id": user, "email_id": email, "arrival_id": arrival, "request_id": requestID} {
		if diagnostic[key] != expected {
			t.Fatalf("missing diagnostic field %s", key)
		}
	}
	if diagnostic["time"] == nil {
		t.Fatal("diagnostic has no observation time")
	}
	output.Reset()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/arrival", strings.NewReader(`{"map":{"user_id":"private","email_id":"private","id":"private"},"pending":{}}`)))
	if strings.Contains(output.String(), `"email_id"`) || strings.Contains(output.String(), `"user_id"`) || strings.Contains(output.String(), `"arrival_id"`) {
		t.Fatal("invalid identities were logged")
	}
}

func (m *internalArrivalMap) Add(context.Context, repository.EmailMessageData) error {
	m.legacyCalls++
	return nil
}
func (*internalArrivalMap) Get(context.Context, uuid.UUID, uuid.UUID, string) (*repository.EmailMessageData, error) {
	return nil, nil
}
func (*internalArrivalMap) Del(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID) error {
	return nil
}
func (m *internalArrivalMap) AdmitArrival(context.Context, repository.EmailMessageData, *repository.PendingArrival) error {
	m.calls++
	return m.err
}

func TestInternalArrivalAcknowledgesOnlyCompletedAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		ack    string
	}{
		{"success", nil, 204, "1"}, {"failure", errors.New("private payload or cipher failure"), 503, ""},
		{"unconfirmed collision", repository.ErrArrivalAdmissionUnconfirmed, 503, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &internalArrivalMap{err: tc.err}
			h := &Handler{EmailMessageMap: m}
			r := gin.New()
			r.POST("/arrival", h.InternalAdmitEmailArrival)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/arrival", strings.NewReader(`{"map":{},"pending":{}}`)))
			if m.calls != 1 || w.Code != tc.status || w.Header().Get("X-Warmbly-Arrival-Durable") != tc.ack || w.Body.Len() != 0 {
				t.Fatalf("calls=%d status=%d body=%q", m.calls, w.Code, w.Body.String())
			}
		})
	}
}

func TestInternalArrivalPreservesLegacyWorkerProtocol(t *testing.T) {
	m := &internalArrivalMap{}
	h := &Handler{EmailMessageMap: m}
	r := gin.New()
	r.PUT("/map", h.InternalPutEmailMessageMap)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/map", strings.NewReader(`{"user_id":"fixture","email_id":"fixture","message_id":"fixture","id":"fixture","thread_id":"fixture"}`)))
	if w.Code != 204 || m.legacyCalls != 1 || m.calls != 0 || w.Header().Get("X-Warmbly-Arrival-Durable") != "" {
		t.Fatalf("legacy contract changed: %d", w.Code)
	}
}
