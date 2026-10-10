//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

func remoteTestDir(t *testing.T) string {
	t.Helper()
	// macOS temporary paths may themselves use system symlinks.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "credentials")
}

func remoteTestToken(now time.Time) remoteToken {
	return remoteToken{"synthetic-access", now.Add(time.Hour), "synthetic-refresh", now.Add(24 * time.Hour)}
}

func remoteTestClient(t *testing.T, base string) *remoteClient {
	t.Helper()
	store, err := openRemoteStore(context.Background(), remoteTestDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.close)
	now := time.Date(2026, 10, 10, 7, 0, 0, 0, time.UTC)
	cred := remoteCredential{base, uuid.NewString(), remoteTestToken(now)}
	key := remoteCredentialKey(base, "laptop")
	store.state.Credentials[key] = cred
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	return &remoteClient{base: base, profile: "laptop", key: key, credential: cred, store: store, http: remoteHTTPClient(), now: func() time.Time { return now }}
}

func assertRemoteError(t *testing.T, err error, exit int, code string) {
	t.Helper()
	var got *remoteError
	if !errors.As(err, &got) || got.Exit != exit || (code != "" && got.Code != code) {
		t.Fatalf("expected exit=%d code=%s, got %v", exit, code, err)
	}
}

func TestRemoteRefreshRotatesBeforeRequestAndPersists(t *testing.T) {
	var client *remoteClient
	var refreshes, reads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/refresh":
			refreshes++
			if r.Header.Get("Authorization") != "" {
				t.Error("refresh must not include an access bearer")
			}
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["refresh_token"] != "synthetic-refresh" {
				t.Error("wrong refresh request")
			}
			next := remoteTestToken(client.now())
			next.AccessToken, next.RefreshToken = "rotated-access", "rotated-refresh"
			_ = json.NewEncoder(w).Encode(next)
		case "/admin/workers":
			reads++
			if r.Header.Get("Authorization") != "Bearer rotated-access" {
				t.Error("stale token was used")
			}
			_, _ = io.WriteString(w, `{"data":[]}`)
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer server.Close()
	client = remoteTestClient(t, server.URL)
	client.credential.Token.AccessTokenExpiresAt = client.now().Add(-time.Second)
	if _, err := client.do(context.Background(), "GET", "/admin/workers", nil); err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 || reads != 1 || client.store.state.Credentials[client.key].Token.RefreshToken != "rotated-refresh" {
		t.Fatal("rotation was not persisted before one authorized request")
	}
	path := client.store.root.Name()
	client.store.close()
	loaded, err := openRemoteStore(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.close()
	if loaded.state.Credentials[client.key].Token.AccessToken != "rotated-access" {
		t.Fatal("rotation did not survive store reopen")
	}
}

func TestRemoteUnauthorizedReadRefreshesOnlyOnce(t *testing.T) {
	var client *remoteClient
	var reads, refreshes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/refresh" {
			refreshes++
			_ = json.NewEncoder(w).Encode(remoteTestToken(client.now()))
			return
		}
		reads++
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"code":"unauthorized","message":"must not leak"}`)
	}))
	defer server.Close()
	client = remoteTestClient(t, server.URL)
	_, err := client.do(context.Background(), "GET", "/admin/workers", nil)
	assertRemoteError(t, err, 4, "unauthorized")
	if refreshes != 1 || reads != 2 || len(client.store.state.Credentials) != 0 {
		t.Fatal("401 retry must be bounded and rejected profile must be forgotten")
	}
}

func TestRemoteWritesNeverRetryAfterUnauthorized(t *testing.T) {
	var requests, refreshes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/refresh" {
			refreshes++
		}
		requests++
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"code":"unauthorized"}`)
	}))
	defer server.Close()
	client := remoteTestClient(t, server.URL)
	_, err := client.do(context.Background(), "POST", "/admin/webhooks/reclaim", nil)
	assertRemoteError(t, err, 4, "unauthorized")
	if requests != 1 || refreshes != 0 || len(client.store.state.Credentials) != 0 {
		t.Fatal("write was repeated or authorization failure retained")
	}
}

func TestRemoteFailedRefreshDropsOnlyAffectedProfile(t *testing.T) {
	for _, response := range []string{`{"code":"unauthorized"}`, `{"access_token":"bad"}`, "not-json"} {
		t.Run(response, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if strings.Contains(response, "unauthorized") {
					w.WriteHeader(401)
				}
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()
			client := remoteTestClient(t, server.URL)
			other := remoteCredentialKey("https://other.example", "laptop")
			client.store.state.Credentials[other] = remoteCredential{URL: "https://other.example", Token: remoteTestToken(client.now())}
			if err := client.refresh(context.Background()); err == nil {
				t.Fatal("failed rotation accepted")
			}
			if calls != 1 || len(client.store.state.Credentials) != 1 || client.store.state.Credentials[other].URL != "https://other.example" {
				t.Fatal("refresh replayed or crossed profile boundary")
			}
		})
	}
}

func TestRemotePermissionAndFreshAuthErrorsDoNotRefresh(t *testing.T) {
	for _, code := range []string{"forbidden", "admin_mfa_required", "reauth_required"} {
		t.Run(code, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"code":"`+code+`","message":"provider secret","request_id":"request-123"}`)
			}))
			defer server.Close()
			client := remoteTestClient(t, server.URL)
			_, err := client.do(context.Background(), "GET", "/admin/fleet/nodes", nil)
			assertRemoteError(t, err, 3, code)
			if calls != 1 || len(client.store.state.Credentials) != 1 || strings.Contains(err.Error(), "provider secret") {
				t.Fatal("denial was bypassed, retried or leaked")
			}
		})
	}
}

func TestRemoteRedirectsNeverForwardCredentials(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { targetCalls++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-access" {
			t.Error("missing original bearer")
		}
		http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := remoteRequest(context.Background(), remoteHTTPClient(), server.URL, "GET", "/admin/workers", "synthetic-access", nil)
	assertRemoteError(t, err, 5, "http_error")
	if targetCalls != 0 {
		t.Fatal("redirect followed")
	}
}

func TestRemoteInstanceAndProfileIsolation(t *testing.T) {
	store, err := openRemoteStore(context.Background(), remoteTestDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	store.state.Servers["laptop"] = "https://one.example"
	base, profile, err := (remoteSelection{Server: "laptop"}).resolve(store)
	if err != nil || base != "https://one.example" || profile != "laptop" {
		t.Fatal("named selection broken")
	}
	_, _, err = (remoteSelection{URL: "https://two.example", Server: "laptop"}).resolve(store)
	assertRemoteError(t, err, 2, "server_mismatch")
	_, _, err = (remoteSelection{}).resolve(store)
	assertRemoteError(t, err, 2, "server_required")
	_, _, err = (remoteSelection{Server: "missing"}).resolve(store)
	assertRemoteError(t, err, 2, "unknown_server")
	_, _, err = (remoteSelection{URL: "https://one.example", Server: "../escape"}).resolve(store)
	assertRemoteError(t, err, 2, "invalid_profile")
	a := remoteCredentialKey("https://one.example", "laptop")
	if a == remoteCredentialKey("https://two.example", "laptop") || a == remoteCredentialKey("https://one.example", "desktop") {
		t.Fatal("credentials share an instance/profile key")
	}
}

func TestRemoteURLValidation(t *testing.T) {
	for _, input := range []string{"https://user:password@example.com", "https://example.com?token=x", "https://example.com#token", "https://example.com/elsewhere", "http://example.com", "file:///tmp/a", "//example.com", "https://example.com:70000", "https://example.com/%2f", "https://example.com?"} {
		if _, err := normalizeRemoteURL(input); err == nil {
			t.Fatalf("unsafe URL accepted: %s", input)
		}
	}
	for input, expected := range map[string]string{"https://EXAMPLE.com:443/": "https://example.com", "http://127.0.0.1:1234/": "http://127.0.0.1:1234", "http://[::1]:1234": "http://[::1]:1234"} {
		actual, err := normalizeRemoteURL(input)
		if err != nil || actual != expected {
			t.Fatalf("normalization failed: %s", input)
		}
	}
}

func TestRemoteStorageRejectsUnsafeModesLinksAndMalformedFiles(t *testing.T) {
	for _, scenario := range []string{"directory-mode", "file-mode", "symlink", "ancestor-link", "lock-link", "hardlink", "corrupt", "duplicate", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			dir := remoteTestDir(t)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "credentials.json")
			safe := `{"version":1,"servers":{},"credentials":{}}`
			switch scenario {
			case "directory-mode":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			case "file-mode":
				if err := os.WriteFile(file, []byte(safe), 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink", "lock-link", "hardlink":
				target := filepath.Join(filepath.Dir(dir), "target")
				if err := os.WriteFile(target, []byte(safe), 0600); err != nil {
					t.Fatal(err)
				}
				if scenario == "lock-link" {
					file = filepath.Join(dir, "lock")
				}
				var err error
				if scenario == "hardlink" {
					err = os.Link(target, file)
				} else {
					err = os.Symlink(target, file)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "ancestor-link":
				alias := filepath.Join(filepath.Dir(dir), "alias")
				if err := os.Symlink(dir, alias); err != nil {
					t.Fatal(err)
				}
				dir = filepath.Join(alias, "subdir")
			default:
				bad := "{" + "synthetic-secret"
				if scenario == "duplicate" {
					bad = `{"version":1,"version":1,"servers":{},"credentials":{}}`
				}
				if scenario == "oversize" {
					bad = strings.Repeat("x", credentialLimit+1)
				}
				if err := os.WriteFile(file, []byte(bad), 0600); err != nil {
					t.Fatal(err)
				}
			}
			store, err := openRemoteStore(context.Background(), dir)
			if store != nil {
				store.close()
			}
			assertRemoteError(t, err, 5, "unsafe_storage")
			if strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("corrupt credential leaked")
			}
		})
	}
}

func TestRemoteStorageAtomicSaveAndLock(t *testing.T) {
	dir := remoteTestDir(t)
	store, err := openRemoteStore(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	store.state.Credentials["key"] = remoteCredential{URL: "https://example.com", Token: remoteTestToken(time.Now())}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]os.FileMode{".": 0700, "credentials.json": 0600, "lock": 0600} {
		info, err := store.root.Lstat(name)
		if err != nil || info.Mode().Perm() != expected {
			t.Fatal("private modes missing")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	other, err := openRemoteStore(ctx, dir)
	if other != nil {
		other.close()
		t.Fatal("parallel lock bypassed")
	}
	assertRemoteError(t, err, 7, "cancelled")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("atomic save left temporary credential files")
	}
}

func TestRemoteRedactionIsRecursiveAndDeterministic(t *testing.T) {
	id := uuid.NewString()
	cursor := paging.EncodeOffset(100)
	payload := `{"summary":{"count":2},"data":[{"id":"` + id + `","status":"pending","has_message_id":false,"payload":{"body":"synthetic-body"},"headers":{"Authorization":"synthetic-bearer"},"password":"synthetic-password","name":"synthetic-provider-token","new_sensitive_field":"synthetic-secret","observed_at":"2026-10-10T07:00:00Z"}],"pagination":{"next_cursor":"` + *cursor + `","has_more":true}}`
	value, err := decodeRemoteJSON([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := writeRemoteJSON(&a, redactRemoteJSON("", value)); err != nil {
		t.Fatal(err)
	}
	_ = writeRemoteJSON(&b, redactRemoteJSON("", value))
	if a.String() != b.String() || strings.Contains(a.String(), "synthetic-") || !strings.Contains(a.String(), id) || !strings.Contains(a.String(), *cursor) || !strings.Contains(a.String(), `"has_message_id":false`) {
		t.Fatal("redaction leaked a value or discarded safe evidence")
	}
	if strings.Contains(a.String(), "headers") || strings.Contains(a.String(), "password") || strings.Contains(a.String(), "payload") {
		t.Fatal("secret subtree survived projection")
	}
	for _, secret := range []string{"wmbly_abcd", "Bearer synthetic", "smtp://user:pass@host", strings.Repeat("s", 43)} {
		if got := safeRemoteString("reason", secret); got != "[REDACTED]" {
			t.Fatal("free-form safe-looking field was trusted")
		}
	}
}

func TestRemoteMalformedInputAndWriteSafeguards(t *testing.T) {
	for _, input := range []string{`{`, `{"enabled":true} {}`, `{"enabled":true,"enabled":false}`, strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)} {
		if _, err := decodeRemoteJSON([]byte(input)); err == nil {
			t.Fatal("malformed, duplicated or unbounded JSON accepted")
		}
	}
	if _, err := readRemoteBody(`{"password":"synthetic"}`); err == nil {
		t.Fatal("inline body accepted")
	}
	if err := parseRemoteFlags(newRemoteFlagSet("whoami"), []string{"--password=synthetic"}); err == nil || strings.Contains(err.Error(), "synthetic") {
		t.Fatal("unsafe flags accepted or printed")
	}
	for _, query := range []remoteQueries{{"a"}, {"limit=10", "limit=20"}, {"password=synthetic"}, {"q=bad\ninput"}} {
		if _, err := parseRemoteQueries(query); err == nil {
			t.Fatal("malformed query accepted")
		}
	}
	id := uuid.NewString()
	requestPath := "/admin/tasks/dead-letters/" + id + "/replay"
	for _, confirmation := range []string{"", "replay", "POST /admin/tasks/dead-letters/another/replay"} {
		err := executeRemoteAdmin(context.Background(), remoteSelection{}, "POST", requestPath, nil, nil, true, confirmation, true)
		assertRemoteError(t, err, 8, "confirmation_required")
	}
	assertRemoteError(t, executeRemoteAdmin(context.Background(), remoteSelection{}, "POST", requestPath, nil, nil, true, "POST "+requestPath, false), 8, "replay_risk_required")
	if err := requireRemoteWrite("POST", requestPath, true, "POST "+requestPath); err != nil {
		t.Fatal("exact confirmation rejected")
	}
	for _, denied := range []struct{ method, path string }{{"GET", "/admin/fleet/nodes/not-a-uuid/logs"}, {"GET", "/admin/../v1/auth/me"}, {"GET", "https://other.example/admin/workers"}, {"POST", "/admin/mailboxes/" + id + "/disable"}, {"POST", "/admin/sends/clear-unknown"}, {"POST", "/admin/broker/reset-offsets"}, {"GET", "/admin/workers?token=synthetic"}, {"GET", "/admin/users/%2fadmin"}} {
		if _, err := findRemoteAdminRoute(denied.method, denied.path); err == nil {
			t.Fatal("unsafe or invented endpoint accepted")
		}
	}
	for _, denied := range []struct{ method, path string }{{"POST", "/admin/auth/device/decide"}, {"POST", "/admin/fleet/join-token"}, {"POST", "/admin/jobs/reclaimer/run"}} {
		assertRemoteError(t, executeRemoteAdmin(context.Background(), remoteSelection{}, denied.method, denied.path, nil, nil, true, denied.method+" "+denied.path, true), 8, "endpoint_unavailable")
	}
}

func TestRemoteDeviceContractBoundToInstance(t *testing.T) {
	grant := remoteDeviceGrant{strings.Repeat("s", 43), "ABCD-EFGH", "https://api.example.com", "/device", 300, 5}
	if err := validateRemoteGrant(grant, "https://api.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := validateRemoteGrant(grant, "https://other.example.com"); err == nil {
		t.Fatal("cross-instance grant accepted")
	}
	grant.VerificationPath = "https://other.example/secret"
	if err := validateRemoteGrant(grant, "https://api.example.com"); err == nil {
		t.Fatal("unsafe verification route accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"status":"pending"}`)
	}))
	defer server.Close()
	if _, err := remoteRequest(context.Background(), remoteHTTPClient(), server.URL, "POST", "/v1/auth/cli/admin/code", "", json.RawMessage(`{"client_name":"test"}`)); err != nil {
		t.Fatal("current backend 201 was not accepted")
	}
}

func TestRemoteLogoutRevokesAndRemovesOnlySelectedInstanceUser(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(strconv.FormatBool(all), func(t *testing.T) {
			var requested string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested = r.URL.Path
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer synthetic-access" {
					t.Error("logout contract mismatch")
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			config, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_CONFIG_HOME", config)
			// macOS uses HOME/Library/Application Support instead of XDG.
			t.Setenv("HOME", config)
			dir, err := remoteStorePath()
			if err != nil {
				t.Fatal(err)
			}
			store, err := openRemoteStore(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			user := uuid.NewString()
			for _, profile := range []string{"laptop", "desktop"} {
				store.state.Credentials[remoteCredentialKey(server.URL, profile)] = remoteCredential{server.URL, user, remoteTestToken(time.Now())}
				store.state.Servers[profile] = server.URL
			}
			other := remoteCredentialKey("https://other.example", "laptop")
			store.state.Credentials[other] = remoteCredential{"https://other.example", user, remoteTestToken(time.Now())}
			if err := store.save(); err != nil {
				t.Fatal(err)
			}
			store.close()
			args := []string{"--server", "laptop"}
			expectedPath, expectedCount := "/v1/auth/logout", 2
			if all {
				args = append(args, "--all", "--write", "--confirm", "POST /v1/auth/logout-all")
				expectedPath, expectedCount = "/v1/auth/logout-all", 1
			}
			if err := runRemoteLogout(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			loaded, err := openRemoteStore(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer loaded.close()
			if requested != expectedPath || len(loaded.state.Credentials) != expectedCount || loaded.state.Credentials[other].URL != "https://other.example" {
				t.Fatal("logout failed to isolate revocation")
			}
		})
	}
}

func TestRemoteErrorClassificationAndBodyRedaction(t *testing.T) {
	for status, exit := range map[int]int{400: 2, 401: 4, 403: 3, 404: 5, 409: 5, 429: 6, 503: 5} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"synthetic-password","message":"synthetic-provider-error","code":"forbidden","request_id":"request-123"}`)
			}))
			defer server.Close()
			_, err := remoteRequest(context.Background(), remoteHTTPClient(), server.URL, "GET", "/admin/workers", "synthetic", nil)
			assertRemoteError(t, err, exit, "forbidden")
			var output bytes.Buffer
			_ = writeRemoteJSON(&output, err)
			if strings.Contains(output.String(), "synthetic-") || !strings.Contains(output.String(), "request-123") {
				t.Fatal("error leaked backend prose or lost safe request ID")
			}
		})
	}
}

func TestRemoteCatalogMatchesActualAdminRoutesAndGuards(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../../internal/api/routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]remoteAdminRoute{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		group, ok := fun.X.(*ast.Ident)
		if !ok || group.Name != "adminRoutes" || len(call.Args) == 0 {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		route := remoteAdminRoute{Method: fun.Sel.Name, Path: "/admin" + path, Permissions: []string{}}
		for _, arg := range call.Args[1:] {
			gate, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			selector, ok := gate.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if selector.Sel.Name == "RequireFreshAuth" {
				route.FreshAuth = true
			}
			if selector.Sel.Name == "RequireAdminPermission" {
				permission, ok := gate.Args[0].(*ast.SelectorExpr)
				if ok {
					route.Permissions = append(route.Permissions, strings.TrimPrefix(permission.Sel.Name, "AdminPerm"))
				}
			}
		}
		actual[route.Method+" "+route.Path] = route
		return true
	})
	if len(actual) != len(remoteAdminRoutes) {
		t.Fatalf("catalog has %d routes, backend has %d; update the bounded catalog", len(remoteAdminRoutes), len(actual))
	}
	for _, route := range remoteAdminRoutes {
		got, ok := actual[route.Method+" "+route.Path]
		if !ok || got.FreshAuth != route.FreshAuth || !reflect.DeepEqual(got.Permissions, route.Permissions) {
			t.Fatalf("catalog guard mismatch: %s %s", route.Method, route.Path)
		}
	}
	for _, helper := range remoteAdminHelpers {
		if _, ok := actual[helper.Method+" "+helper.Path]; !ok {
			t.Fatalf("helper invented an endpoint: %s", helper.Command)
		}
	}
}
