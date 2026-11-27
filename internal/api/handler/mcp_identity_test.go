package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// An MCP client names the server from resource_name before sign-in and from
// serverInfo.title after, and shows serverInfo.icons beside it.
func TestMCPServerNamesItself(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/mcp", nil)
	mcpCommon(c, jsonRPCRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize"}, "")
	var initialized struct {
		Result struct {
			ServerInfo struct {
				Name  string `json:"name"`
				Title string `json:"title"`
				Icons []struct {
					Src string `json:"src"`
				} `json:"icons"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &initialized); err != nil {
		t.Fatal(err)
	}
	info := initialized.Result.ServerInfo
	if info.Name != "warmbly" || info.Title != "Warmbly" || len(info.Icons) == 0 || !strings.HasPrefix(info.Icons[0].Src, "https://") {
		t.Fatalf("serverInfo %+v", info)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	(&Handler{}).OAuthProtectedResourceMetadata(c)
	var metadata struct {
		ResourceName string `json:"resource_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.ResourceName != "Warmbly" {
		t.Fatalf("resource_name %q", metadata.ResourceName)
	}
}
