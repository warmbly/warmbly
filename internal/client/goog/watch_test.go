package goog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestWatchRefusalKeepsGooglesReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Invalid topicName does not match projects/mailboxes/topics/*"}}`))
	}))
	defer srv.Close()
	svc, err := gmail.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"), option.WithHTTPClient(srv.Client()), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Client{srv: svc}).Watch(t.Context(), "projects/grants/topics/gmail")
	var mailErr *errx.MailError
	if err == nil || !strings.Contains(err.Error(), "400 Invalid topicName") || !errors.As(err, &mailErr) {
		t.Fatalf("watch refusal = %v, want Google's reason and a classifiable MailError", err)
	}
}
