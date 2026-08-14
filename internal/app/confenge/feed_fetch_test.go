package confenge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeedFetcherRejectsOversizedHTTPPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()

	fetcher := FeedFetcher{MaxBytes: 4, HTTPClient: server.Client()}
	_, err := fetcher.Fetch(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "payload exceeds max size") {
		t.Fatalf("expected oversized payload rejection, got %v", err)
	}
}
