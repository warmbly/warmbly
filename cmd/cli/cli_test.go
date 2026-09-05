package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/config"
	"github.com/warmbly/warmbly/internal/models"
)

func TestFillPath(t *testing.T) {
	specs := []argSpec{{Name: "id"}, {Name: "step"}}
	got, err := fillPath("/campaigns/{id}/steps/{step}", specs, []string{"abc", "def"})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if got != "/campaigns/abc/steps/def" {
		t.Errorf("path = %q", got)
	}
	if _, err := fillPath("/campaigns/{id}", specs[:1], []string{"  "}); err == nil {
		t.Error("an empty argument must be rejected rather than producing /campaigns/")
	}
	// A tool name can contain characters that need escaping in a path.
	got, err = fillPath("/ai/tools/{name}/call", []argSpec{{Name: "name"}}, []string{"a b"})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if !strings.Contains(got, "a%20b") {
		t.Errorf("path segment was not escaped: %q", got)
	}
}

// Every spec has to produce a runnable command: one {} per positional argument
// and no leftovers, or the command is dead on arrival at runtime.
func TestEverySpecPathMatchesItsArguments(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range resourceSpecs() {
		if seen[r.Name] {
			t.Errorf("two resources are called %q", r.Name)
		}
		seen[r.Name] = true

		endpoints := map[string]bool{}
		for _, e := range r.Endpoints {
			if endpoints[e.Name] {
				t.Errorf("%s has two %q commands", r.Name, e.Name)
			}
			endpoints[e.Name] = true

			if e.Method == "" || e.Path == "" || e.Short == "" {
				t.Errorf("%s %s is missing a method, path or summary", r.Name, e.Name)
			}
			if !strings.HasPrefix(e.Path, "/") {
				t.Errorf("%s %s path %q must be /v1-relative", r.Name, e.Name, e.Path)
			}
			placeholders := strings.Count(e.Path, "{")
			if placeholders != len(e.Args) {
				t.Errorf("%s %s has %d placeholders and %d arguments", r.Name, e.Name, placeholders, len(e.Args))
			}
			args := make([]string, len(e.Args))
			for i := range e.Args {
				args[i] = "x"
				if e.Args[i].Help == "" {
					t.Errorf("%s %s argument %q has no help", r.Name, e.Name, e.Args[i].Name)
				}
			}
			if _, err := fillPath(e.Path, e.Args, args); err != nil {
				t.Errorf("%s %s: %v", r.Name, e.Name, err)
			}
			if e.Method == http.MethodGet && e.Body != bodyNone {
				t.Errorf("%s %s is a GET with a body", r.Name, e.Name)
			}
			flags := map[string]bool{}
			for _, fl := range e.Flag {
				if flags[fl.Name] {
					t.Errorf("%s %s declares --%s twice", r.Name, e.Name, fl.Name)
				}
				flags[fl.Name] = true
				if fl.Help == "" {
					t.Errorf("%s %s flag --%s has no help", r.Name, e.Name, fl.Name)
				}
				// -h is cobra's help shorthand; claiming it breaks the command.
				if fl.Short == "h" {
					t.Errorf("%s %s cannot use -h for --%s", r.Name, e.Name, fl.Name)
				}
			}
		}
	}
}

// Building the whole command tree catches the failures cobra reports by
// panicking: a duplicate shorthand, a bad group id.
func TestCommandTreeBuilds(t *testing.T) {
	f := NewFactory()
	root := newRootCmd(f)
	if len(root.Commands()) == 0 {
		t.Fatal("no commands registered")
	}
	for _, c := range root.Commands() {
		if c.GroupID == "" && c.Name() != "help" && c.Name() != "completion" {
			t.Errorf("%s has no group, so it falls out of the grouped help", c.Name())
		}
		for _, sub := range c.Commands() {
			if sub.Short == "" {
				t.Errorf("%s %s has no summary", c.Name(), sub.Name())
			}
		}
	}
}

func TestBuildFields(t *testing.T) {
	body, err := buildFields(
		[]string{"name=Jane", "note=true"},
		[]string{"limit=40", "active=true", "missing=null", "tags[]=a", "tags[]=b", "nested[key]=v"},
		strings.NewReader(""),
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if body["name"] != "Jane" {
		t.Errorf("-f name should stay a string, got %#v", body["name"])
	}
	if body["note"] != "true" {
		t.Errorf("-f keeps values literal, got %#v", body["note"])
	}
	if body["limit"] != int64(40) {
		t.Errorf("-F limit should be a number, got %#v", body["limit"])
	}
	if body["active"] != true {
		t.Errorf("-F active should be a bool, got %#v", body["active"])
	}
	if body["missing"] != nil {
		t.Errorf("-F null should be null, got %#v", body["missing"])
	}
	tags, _ := body["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("key[] should build an array, got %#v", body["tags"])
	}
	nested, _ := body["nested"].(map[string]any)
	if nested["key"] != "v" {
		t.Errorf("key[sub] should nest, got %#v", body["nested"])
	}

	if _, err := buildFields([]string{"broken"}, nil, strings.NewReader("")); err == nil {
		t.Error("a field with no = must be rejected")
	}
}

func TestSplitArgs(t *testing.T) {
	got, err := splitArgs(`campaign list --status "in progress" --q 'x y'`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	want := []string{"campaign", "list", "--status", "in progress", "--q", "x y"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
	if _, err := splitArgs(`unbalanced "`); err == nil {
		t.Error("an unbalanced quote must be an error, not a silent truncation")
	}
	// An empty quoted argument is still an argument.
	if got, _ := splitArgs(`a "" b`); len(got) != 3 {
		t.Errorf("empty quoted argument was dropped: %#v", got)
	}
}

func TestExpandAliases(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	cfg := &config.Config{Aliases: map[string]string{"hot": "campaign list --status active"}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	f := NewFactory()
	got := expandAliases(f, []string{"hot", "--json"})
	want := []string{"campaign", "list", "--status", "active", "--json"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("expanded to %v, want %v", got, want)
	}
	// A non-alias is untouched.
	if got := expandAliases(f, []string{"campaign", "list"}); got[0] != "campaign" {
		t.Errorf("a real command was rewritten: %v", got)
	}
}

func TestParseScopes(t *testing.T) {
	if mask, err := parseScopes(""); err != nil || mask != models.APIPermFullAccess {
		t.Errorf("empty should mean full access, got %d %v", mask, err)
	}
	if mask, err := parseScopes("read-only"); err != nil || mask != models.APIPermReadOnly {
		t.Errorf("read-only preset = %d %v", mask, err)
	}
	mask, err := parseScopes("read_campaigns,SEND_CAMPAIGNS")
	if err != nil {
		t.Fatalf("named scopes: %v", err)
	}
	if mask&models.APIPermReadCampaigns == 0 || mask&models.APIPermSendCampaigns == 0 {
		t.Errorf("named scopes did not resolve: %d", mask)
	}
	if _, err := parseScopes("not_a_scope"); err == nil {
		t.Error("an unknown scope must be named, not dropped")
	}
}

// The device flow is the whole sign-in, so it gets an end-to-end test against
// a server that behaves like the real one: pending, then approved once.
func TestDeviceFlow(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/cli/code":
			var req models.CLIAuthStartRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.CLIVersion == "" || req.Scopes == 0 {
				t.Errorf("the CLI must identify itself and name its scopes: %+v", req)
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"device_code":"dc","user_code":"ABCD-EFGH","verification_uri":"https://app.example/cli","verification_uri_complete":"https://app.example/cli?code=ABCD-EFGH","expires_in":600,"interval":1}`)
		case "/v1/auth/cli/poll":
			polls++
			if polls < 2 {
				fmt.Fprint(w, `{"status":"pending"}`)
				return
			}
			fmt.Fprint(w, `{"status":"approved","token":"wmbly_minted","user_email":"jane@example.com","organization_name":"Acme","api_key_id":"key-1","scope_names":["READ_CAMPAIGNS"]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := api.New(srv.URL, "", "test")
	start, err := startDeviceFlow(context.Background(), client, "laptop", models.APIPermReadOnly)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if start.UserCode != "ABCD-EFGH" {
		t.Errorf("user code = %q", start.UserCode)
	}

	result, err := pollDeviceFlow(context.Background(), client, start)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if polls < 2 {
		t.Errorf("the client stopped polling before approval")
	}
	if result.Token != "wmbly_minted" || result.UserEmail != "jane@example.com" {
		t.Errorf("approval payload lost: %+v", result)
	}
}

func TestDeviceFlowStopsOnDenial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"denied"}`)
	}))
	defer srv.Close()

	client := api.New(srv.URL, "", "test")
	_, err := pollDeviceFlow(context.Background(), client, &deviceStart{DeviceCode: "dc", UserCode: "X", Interval: 1, ExpiresIn: 60})
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Errorf("a denial must end the wait, got %v", err)
	}
}

func TestAppURLFromVerification(t *testing.T) {
	// The instance hands back its own APP_URL with /cli on the end, which is
	// exact where a hostname guess is not.
	if got := appURLFromVerification("https://app.acme.dev/cli"); got != "https://app.acme.dev" {
		t.Errorf("got %q", got)
	}
	if got := appURLFromVerification("http://localhost:5173/cli"); got != "http://localhost:5173" {
		t.Errorf("got %q", got)
	}
	if got := appURLFromVerification(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestDashboardURLPrefersWhatTheInstanceReported(t *testing.T) {
	r := &config.Resolved{Host: "acme.dev", Entry: &config.Host{AppURL: "https://warmbly.acme.dev/"}}
	if got := dashboardURL(r); got != "https://warmbly.acme.dev" {
		t.Errorf("got %q, want the reported origin", got)
	}
	// With nothing reported, fall back to the layout the installer writes.
	if got := dashboardURL(&config.Resolved{Host: "acme.dev"}); got != "https://app.acme.dev" {
		t.Errorf("fallback = %q", got)
	}
}
