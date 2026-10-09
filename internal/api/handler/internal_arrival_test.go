package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

type internalArrivalMap struct {
	calls, legacyCalls int
	err                error
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
