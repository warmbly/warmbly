package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/cli/iostreams"
	"github.com/warmbly/warmbly/internal/models"
)

type capturedRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
	org    string
}

// runAgainst runs one CLI invocation against a recording server and returns the request it made.
func runAgainst(t *testing.T, args ...string) capturedRequest {
	t.Helper()
	var got capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.Query()
		got.body, _ = io.ReadAll(r.Body)
		got.org = r.Header.Get("Warmbly-Organization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	t.Setenv("WARMBLY_CONFIG_DIR", t.TempDir())
	t.Setenv("WARMBLY_TOKEN", "wmbly_test")
	t.Setenv("WARMBLY_API_URL", srv.URL)
	var out bytes.Buffer
	f := &Factory{IO: &iostreams.IOStreams{In: strings.NewReader(""), Out: &out, ErrOut: &out}}
	root := newRootCmd(f)
	root.SetArgs(append(args, "--yes", "--json"))
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("warmbly %s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	if got.method == "" {
		t.Fatalf("warmbly %s made no request", strings.Join(args, " "))
	}
	return got
}

// strictDecode fails when the body carries a key the server's struct does not read.
func strictDecode(t *testing.T, body []byte, dst any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		t.Fatalf("body %s does not decode into %T: %v", body, dst, err)
	}
}

func bodyKeys(t *testing.T, body []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body %s is not an object: %v", body, err)
	}
	return m
}

// Each command's body has to be one the endpoint binds (issue #650).
func TestCommandBodiesMatchTheAPI(t *testing.T) {
	t.Run("contact create", func(t *testing.T) {
		r := runAgainst(t, "contact", "create", "--email", "jane@example.com", "--first-name", "Jane")
		var c models.AddContact
		strictDecode(t, r.body, &c)
		if c.Email != "jane@example.com" || c.FirstName != "Jane" {
			t.Fatalf("decoded %+v", c)
		}
	})

	t.Run("mailbox send", func(t *testing.T) {
		r := runAgainst(t, "mailbox", "send", "MB", "--to", "a@example.com,b@example.com", "--subject", "Hi", "--body", "<p>Hi</p>")
		var req emailsend.SendEmailRequest
		strictDecode(t, r.body, &req)
		if len(req.To) != 2 || req.Subject != "Hi" || req.BodyHTML != "<p>Hi</p>" {
			t.Fatalf("decoded %+v", req)
		}
	})

	for _, cmd := range [][]string{{"add-step", "C"}, {"edit-step", "C", "S"}} {
		t.Run("campaign "+cmd[0], func(t *testing.T) {
			args := append([]string{"campaign"}, cmd...)
			args = append(args, "--subject", "Hi", "--body-plain", "Hi", "--wait-after", "3")
			var step models.UpdateSequence
			strictDecode(t, runAgainst(t, args...).body, &step)
			if step.Subject == nil || step.WaitAfter == nil || *step.WaitAfter != 3 {
				t.Fatalf("decoded %+v", step)
			}
		})
	}

	t.Run("mailbox set-tracking", func(t *testing.T) {
		r := runAgainst(t, "mailbox", "set-tracking", "MB", "--domain", "t.example.com")
		if r.method != http.MethodPatch || r.query.Get("domain") != "t.example.com" || len(r.body) != 0 {
			t.Fatalf("sent %s ?%s body %q", r.method, r.query.Encode(), r.body)
		}
	})

	t.Run("form set-domain", func(t *testing.T) {
		m := bodyKeys(t, runAgainst(t, "form", "set-domain", "--domain", "f.example.com").body)
		if m["forms_domain"] != "f.example.com" || len(m) != 1 {
			t.Fatalf("sent %v", m)
		}
	})

	t.Run("campaign test", func(t *testing.T) {
		m := bodyKeys(t, runAgainst(t, "campaign", "test", "C", "--mailbox", "MB", "--to", "you@example.com").body)
		if m["recipient"] != "you@example.com" || m["account_id"] != "MB" || len(m) != 2 {
			t.Fatalf("sent %v", m)
		}
	})

	t.Run("task create", func(t *testing.T) {
		r := runAgainst(t, "task", "create", "--title", "Call", "--due", "2026-10-01T09:00:00Z")
		var task models.CreateCRMTask
		strictDecode(t, r.body, &task)
		if task.DueDate == nil || task.Title != "Call" {
			t.Fatalf("decoded %+v", task)
		}
	})

	t.Run("advisor snooze", func(t *testing.T) {
		var req models.AdvisorSnoozeRequest
		strictDecode(t, runAgainst(t, "advisor", "snooze", "R", "--days", "7").body, &req)
		if req.Days != 7 {
			t.Fatalf("decoded %+v", req)
		}
	})

	t.Run("mailbox warmup-appeal", func(t *testing.T) {
		m := bodyKeys(t, runAgainst(t, "mailbox", "warmup-appeal", "MB", "--reason", "fixed DNS").body)
		if m["reason"] != "fixed DNS" || len(m) != 1 {
			t.Fatalf("sent %v", m)
		}
	})

	t.Run("integration push", func(t *testing.T) {
		var sel models.ContactSelection
		strictDecode(t, runAgainst(t, "integration", "push", "C", "--contacts", "a,b").body, &sel)
		if len(sel.Contacts) != 2 {
			t.Fatalf("decoded %+v", sel)
		}
	})

	t.Run("migration create", func(t *testing.T) {
		r := runAgainst(t, "migration", "create", "--input", `{"provider":"smartlead","credentials":[{"api_key":"k"}]}`)
		var m models.NewToolMigration
		strictDecode(t, r.body, &m)
		if r.path != "/v1/migrations" || m.Provider != "smartlead" || len(m.Credentials) != 1 || m.Credentials[0]["api_key"] != "k" {
			t.Fatalf("sent %s %+v", r.path, m)
		}
	})

	t.Run("migration start", func(t *testing.T) {
		r := runAgainst(t, "migration", "start", "M", "--kinds", "campaigns,leads", "--in-flight", "skip", "--warmup")
		var s models.StartToolMigration
		strictDecode(t, r.body, &s)
		if r.path != "/v1/migrations/M/start" || len(s.Options.Kinds) != 2 || s.Options.InFlight != models.ToolMigrationSkip || !s.Options.Warmup {
			t.Fatalf("sent %s %+v", r.path, s)
		}
	})

	t.Run("agency migration start", func(t *testing.T) {
		r := runAgainst(t, "agency", "migration", "start", "A", "M", "--input", `{"options":{"kinds":["leads"]},"targets":[{"workspace_id":"w","key":0,"create_name":"Acme"}]}`)
		var s models.StartToolMigration
		strictDecode(t, r.body, &s)
		if r.path != "/v1/agency/A/migrations/M/start" || len(s.Targets) != 1 {
			t.Fatalf("sent %s %+v", r.path, s)
		}
	})

	t.Run("agency clients create", func(t *testing.T) {
		r := runAgainst(t, "agency", "clients", "create", "A", "--name", "Acme")
		m := bodyKeys(t, r.body)
		if r.path != "/v1/agency/A/clients" || m["name"] != "Acme" || len(m) != 1 {
			t.Fatalf("sent %s %v", r.path, m)
		}
	})

	t.Run("organization header", func(t *testing.T) {
		if r := runAgainst(t, "campaign", "list", "--organization", "org-1"); r.org != "org-1" {
			t.Fatalf("flag sent header %q", r.org)
		}
		t.Setenv("WARMBLY_ORGANIZATION", "org-2")
		if r := runAgainst(t, "campaign", "list"); r.org != "org-2" {
			t.Fatalf("env sent header %q", r.org)
		}
		if r := runAgainst(t, "campaign", "list", "--organization", "org-3"); r.org != "org-3" {
			t.Fatalf("flag did not win over env: %q", r.org)
		}
	})

	t.Run("oauth-app create", func(t *testing.T) {
		var w models.OAuthApplicationWrite
		strictDecode(t, runAgainst(t, "oauth-app", "create", "--name", "App", "--scopes", "3").body, &w)
		if w.Scopes != 3 || w.Name != "App" {
			t.Fatalf("decoded %+v", w)
		}
	})
}
