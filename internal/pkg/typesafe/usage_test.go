package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestUsageLogsMetadataWithoutContentOrKey(t *testing.T) {
	var output bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&output)
	t.Cleanup(func() { log.Logger = previous })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(goodAnswer))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	ctx := WithUsage(context.Background(), "inbox_tagging", "workspace-id")
	if _, err := c.Ask(ctx, "private message body", testQuestions()); err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Event        string `json:"event"`
		Feature      string `json:"feature"`
		Organization string `json:"organization_id"`
		Tokens       int    `json:"input_tokens"`
		Questions    int    `json:"questions"`
		Status       int    `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Event != "typesafe_usage" || entry.Feature != "inbox_tagging" || entry.Organization != "workspace-id" || entry.Tokens != 1300 || entry.Questions != 1 || entry.Status != 200 {
		t.Fatalf("missing usage attribution: %+v", entry)
	}
	for _, private := range []string{"test-key", "private message body", "What is this message?"} {
		if strings.Contains(output.String(), private) {
			t.Fatalf("usage logs leaked %q", private)
		}
	}
}

func TestUsageFeaturePreservesWorkspaceScope(t *testing.T) {
	ctx := WithUsage(context.Background(), "advisor", "workspace-id")
	ctx = WithUsage(ctx, "copy_judgment", "")
	if featureFrom(ctx) != "copy_judgment" || ctx.Value(organizationKey{}) != "workspace-id" {
		t.Fatal("feature annotation lost workspace attribution")
	}
}
