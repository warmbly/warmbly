package aitools

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

type recordedCall struct {
	method, path string
	query        url.Values
	body         []byte
}

type fakeAPI struct {
	status int
	resp   string
	calls  []recordedCall
}

func (f *fakeAPI) Do(_ context.Context, method, path string, query url.Values, body []byte) (int, []byte, error) {
	f.calls = append(f.calls, recordedCall{method, path, query, body})
	return f.status, []byte(f.resp), nil
}

func knowledgeRegistry() *Registry {
	r := NewRegistry()
	Deps{}.registerKnowledgeTools(r)
	return r
}

func memberInv(api APIDispatcher) Invocation {
	return Invocation{OrgID: uuid.New(), UserID: uuid.New(), OrgPerms: models.RolePermissions[models.RoleAdmin], API: api}
}

func TestAPIToolsNeedTheCallersAPIAccess(t *testing.T) {
	r := knowledgeRegistry()
	for _, inv := range []Invocation{memberInv(nil), {OrgID: uuid.New(), IsAPIKey: true, APIPerms: ^uint64(0)}} {
		if r.Permits(inv, "api_read") || r.Permits(inv, "api_write") {
			t.Fatal("API tools must be hidden without the caller's own API access")
		}
		if !r.Permits(inv, "search_docs") || !r.Permits(inv, "find_api") {
			t.Fatal("docs and API discovery need no API access")
		}
	}
	if !r.Permits(memberInv(&fakeAPI{}), "api_write") {
		t.Fatal("a member with API access should get api_write")
	}
	if tool, _ := r.Get("api_write"); !tool.AlwaysAsk {
		t.Fatal("api_write must never be always allowed")
	}
}

func TestAPIReadAndWriteKeepToTheirKind(t *testing.T) {
	r := knowledgeRegistry()
	api := &fakeAPI{status: 200, resp: `{"data":[]}`}
	inv := memberInv(api)
	ctx := context.Background()
	for _, tc := range []struct {
		tool, args, wantErr string
	}{
		{"api_read", `{"method":"PATCH","path":"/contacts/x1"}`, "api_write"},
		{"api_write", `{"method":"GET","path":"/campaigns"}`, "api_read"},
		{"api_write", `{"method":"POST","path":"/contacts/search","body":{}}`, "api_read"},
		{"api_write", `{"method":"POST","path":"/unibox/reply","body":{}}`, "not available"},
		{"api_write", `{"method":"POST","path":"/campaigns/x1/start"}`, "not available"},
		{"api_read", `{"path":"/api-keys"}`, "not available"},
		{"api_read", `{"path":"/auth/me"}`, "not available"},
		{"api_read", `{"path":"/contacts/../auth/me"}`, "path must be"},
		{"api_read", `{"path":"/internal/dek"}`, "no documented endpoint"},
		{"api_read", `{"path":"/campaigns","body":{"q":"x"}}`, "no body"},
	} {
		_, err := r.Call(ctx, inv, tc.tool, json.RawMessage(tc.args))
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s %s: err = %v, want %q", tc.tool, tc.args, err, tc.wantErr)
		}
	}
	if len(api.calls) != 0 {
		t.Fatalf("refused calls reached the API: %+v", api.calls)
	}

	if _, err := r.Call(ctx, inv, "api_read", json.RawMessage(`{"path":"/v1/campaigns","query":{"limit":20,"q":"spring","archived":false}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Call(ctx, inv, "api_read", json.RawMessage(`{"method":"POST","path":"/contacts/search","body":{"query":"acme"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Call(ctx, inv, "api_write", json.RawMessage(`{"method":"PATCH","path":"/contacts/x1","body":{"first_name":"Ada"}}`)); err != nil {
		t.Fatal(err)
	}
	got := api.calls
	if len(got) != 3 || got[0].path != "/campaigns" || got[0].query.Get("limit") != "20" || got[0].query.Get("archived") != "false" ||
		got[1].method != "POST" || string(got[1].body) != `{"query":"acme"}` ||
		got[2].method != "PATCH" || got[2].path != "/contacts/x1" {
		t.Fatalf("calls = %+v", got)
	}
}

func TestAPIResultsWithholdSecretsAndSurfaceErrors(t *testing.T) {
	r := knowledgeRegistry()
	ctx := context.Background()
	api := &fakeAPI{status: 200, resp: `{"data":[{"id":"1","name":"Clay","inbound_url":"https://x/in/abc","config":{"api_key":"sk-1","region":"eu"},"access_token":"t"}]}`}
	out, err := r.Call(ctx, memberInv(api), "api_read", json.RawMessage(`{"path":"/integrations/connections"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"abc", "sk-1", `"t"`} {
		if strings.Contains(out, leaked) {
			t.Fatalf("secret %s reached the model: %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"region":"eu"`) || !strings.Contains(out, `"name":"Clay"`) {
		t.Fatalf("ordinary fields were withheld: %s", out)
	}

	api = &fakeAPI{status: 403, resp: `{"code":"forbidden","message":"API key lacks WRITE_CONTACTS","request_id":"r1"}`}
	_, err = r.Call(ctx, memberInv(api), "api_write", json.RawMessage(`{"method":"DELETE","path":"/contacts/x1"}`))
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "WRITE_CONTACTS") {
		t.Fatalf("error status: %v", err)
	}
}

func TestKnowledgeToolsAnswerFromTheDocsAndSpec(t *testing.T) {
	r := knowledgeRegistry()
	inv := memberInv(nil)
	ctx := context.Background()
	out, err := r.Call(ctx, inv, "search_docs", json.RawMessage(`{"query":"warmup pool quarantine"}`))
	if err != nil || !strings.Contains(out, "guides/") {
		t.Fatalf("search_docs: %v %s", err, out)
	}
	out, err = r.Call(ctx, inv, "find_api", json.RawMessage(`{"query":"reply template"}`))
	if err != nil || !strings.Contains(out, "/templates") {
		t.Fatalf("find_api: %v %s", err, out)
	}
	out, err = r.Call(ctx, inv, "find_api", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, `"areas"`) {
		t.Fatalf("find_api areas: %v %s", err, out)
	}
	out, err = r.Call(ctx, inv, "describe_api", json.RawMessage(`{"method":"PATCH","path":"/contacts/7f9c"}`))
	if err != nil || !strings.Contains(out, `"/contacts/{id}"`) {
		t.Fatalf("describe_api: %v %s", err, out)
	}
}
