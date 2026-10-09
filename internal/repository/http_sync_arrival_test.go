package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPArrivalAdmissionUpgradeAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		ack                  string
		unsupported, success bool
	}{
		{"old-backend", 404, "", true, false}, {"old-method", 405, "", true, false},
		{"durable", 204, "1", false, true}, {"unconfirmed", 204, "", false, false},
		{"backend-failure", 503, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/internal/email-message-map/arrival" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("incorrect internal protocol")
				}
				w.Header().Set("X-Warmbly-Arrival-Durable", tc.ack)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			repo, err := NewHTTPEmailMessageMapRepository(srv.URL, "fixture-token")
			if err != nil {
				t.Fatal(err)
			}
			err = repo.(ArrivalAdmission).AdmitArrival(context.Background(), EmailMessageData{}, &PendingArrival{})
			if errors.Is(err, ErrArrivalOutboxUnsupported) != tc.unsupported || (err == nil) != tc.success {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestHTTPArrivalAdmissionResponseLossDoesNotDowngrade(t *testing.T) {
	var committed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Error("ambiguous admission fell back to legacy write")
		}
		committed.Store(true)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()
	r, err := NewHTTPEmailMessageMapRepository(srv.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	err = r.(ArrivalAdmission).AdmitArrival(context.Background(), EmailMessageData{}, &PendingArrival{})
	if err == nil || errors.Is(err, ErrArrivalOutboxUnsupported) || !committed.Load() {
		t.Fatalf("response loss treated as unsupported: committed=%v err=%v", committed.Load(), err)
	}
}
