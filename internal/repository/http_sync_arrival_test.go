package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
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

func TestHTTPArrivalOwnershipLossRequiresExactNonAcknowledgedMarker(t *testing.T) {
	mailbox := uuid.NewString()
	for _, tc := range []struct {
		name, requested, ack string
		status               int
		markers              []string
		lost                 bool
	}{
		{"matching", mailbox, "", 503, []string{mailbox}, true},
		{"missing", mailbox, "", 503, nil, false},
		{"other-mailbox", mailbox, "", 503, []string{uuid.NewString()}, false},
		{"invalid", mailbox, "", 503, []string{"private error"}, false},
		{"duplicate", mailbox, "", 503, []string{mailbox, mailbox}, false},
		{"invalid-request", "invalid", "", 503, []string{"invalid"}, false},
		{"nil-mailbox", uuid.Nil.String(), "", 503, []string{uuid.Nil.String()}, false},
		{"wrong-status", mailbox, "", 500, []string{mailbox}, false},
		{"auth-failure", mailbox, "", 401, []string{mailbox}, false},
		{"contradictory-durable", mailbox, "1", 503, []string{mailbox}, false},
		{"not-durable-success", mailbox, "", 204, []string{mailbox}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("request was not authenticated")
				}
				for _, marker := range tc.markers {
					w.Header().Add("X-Warmbly-Arrival-Ownership-Lost", marker)
				}
				w.Header().Set("X-Warmbly-Arrival-Durable", tc.ack)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			repo, err := NewHTTPEmailMessageMapRepository(srv.URL, "fixture-token")
			if err != nil {
				t.Fatal(err)
			}
			err = repo.(ArrivalAdmission).AdmitArrival(t.Context(), EmailMessageData{EmailID: tc.requested}, &PendingArrival{})
			if err == nil || errors.Is(err, ErrArrivalMailboxOwnershipLost) != tc.lost || errors.Is(err, ErrArrivalOutboxUnsupported) {
				t.Fatalf("unexpected admission: %v", err)
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

func TestHTTPArrivalRedirectCannotAuthorizeMailboxRetirement(t *testing.T) {
	mailbox := uuid.NewString()
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Warmbly-Arrival-Ownership-Lost", mailbox)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer redirected.Close()
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected.URL, http.StatusTemporaryRedirect)
	}))
	defer original.Close()
	repo, err := NewHTTPEmailMessageMapRepository(original.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	err = repo.(ArrivalAdmission).AdmitArrival(t.Context(), EmailMessageData{EmailID: mailbox}, &PendingArrival{})
	if err == nil || errors.Is(err, ErrArrivalMailboxOwnershipLost) || errors.Is(err, ErrArrivalOutboxUnsupported) {
		t.Fatalf("redirected endpoint authorized retirement: %v", err)
	}
}
