package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// toolServer answers initialize and then tools/call with result.
func toolServer(t *testing.T, result string) *httptest.Server {
	t.Helper()
	t.Setenv("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS", "true")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &msg)
		if msg.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		res := `{"protocolVersion":"2025-03-26"}`
		if msg.Method == "tools/call" {
			res = result
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + "1" + `,"result":` + res + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCallToolResultKeepsStructuredContent(t *testing.T) {
	srv := toolServer(t, `{"content":[{"type":"text","text":"2 results"}],"structuredContent":{"results":[{"url":"https://acme.com/"}]}}`)
	res, err := NewClient(5*time.Second).CallToolResult(t.Context(), srv.URL, "", "search", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "2 results" || !strings.Contains(string(res.Structured), "https://acme.com/") {
		t.Fatalf("result = %+v", res)
	}
}

func TestCallToolReportsAnErrorWhateverItsContentShape(t *testing.T) {
	srv := toolServer(t, `{"content":"see https://docs.example/errors","isError":true}`)
	if _, err := NewClient(5*time.Second).CallTool(t.Context(), srv.URL, "", "search", nil); err == nil {
		t.Fatal("an isError answer came back as a result")
	}
}
