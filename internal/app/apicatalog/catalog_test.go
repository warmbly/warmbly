package apicatalog

import (
	"strings"
	"testing"
)

func defaultCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Operations()) < 400 {
		t.Fatalf("indexed only %d operations", len(c.Operations()))
	}
	return c
}

// concrete fills every path parameter with a plausible id.
func concrete(template string) string {
	segs := splitPath(template)
	for i, s := range segs {
		if isParam(s) {
			segs[i] = "0f8fad5b-d9cb-469f-a165-70867728950e"
		}
	}
	return "/" + strings.Join(segs, "/")
}

func TestEveryDocumentedOperationIsReachableByItsConcretePath(t *testing.T) {
	c := defaultCatalog(t)
	for _, o := range c.Operations() {
		got, err := c.Match(o.Method, concrete(o.Path))
		if err != nil {
			t.Fatalf("%s: %v", o.Key(), err)
		}
		// A literal route documented at the same shape wins, which is also what the router does.
		if got != o && !strings.Contains(o.Path, "{") {
			t.Fatalf("%s resolved to %s", o.Key(), got.Key())
		}
	}
	if o, _ := c.Match("POST", "/contacts/search"); o == nil || o.Path != "/contacts/search" {
		t.Fatal("a literal segment should win over a parameter")
	}
	if o, _ := c.Match("get", "/v1/campaigns"); o == nil || o.Path != "/campaigns" {
		t.Fatal("the version prefix and method case should not matter")
	}
}

func TestPolicyNamesOnlyDocumentedOperations(t *testing.T) {
	c := defaultCatalog(t)
	for key := range sendOperations {
		m, p, _ := strings.Cut(key, " ")
		o := c.Find(m, p)
		if o == nil {
			t.Fatalf("send operation %s is not in the spec; update the policy with the route", key)
		}
		if o.Blocked == "" {
			t.Fatalf("%s sends mail but is callable", key)
		}
	}
	for key := range readPosts {
		m, p, _ := strings.Cut(key, " ")
		if o := c.Find(m, p); o == nil || o.Kind != KindRead {
			t.Fatalf("read POST %s is not in the spec", key)
		}
	}
	for _, a := range blockedAreas {
		found := false
		for _, o := range c.Operations() {
			if strings.HasPrefix(o.Path, a.prefix) {
				found = true
				if o.Blocked == "" {
					t.Fatalf("%s is in a blocked area but callable", o.Key())
				}
			}
		}
		if !found {
			t.Fatalf("blocked area %s matches no operation", a.prefix)
		}
	}
}

func TestOnlyGetAndListedPostsRead(t *testing.T) {
	for _, o := range defaultCatalog(t).Operations() {
		want := KindWrite
		if o.Method == "GET" || readPosts[o.Key()] {
			want = KindRead
		}
		if o.Kind != want {
			t.Fatalf("%s classified %s", o.Key(), o.Kind)
		}
	}
}

func TestMatchRefusesAnythingButAPlainPath(t *testing.T) {
	c := defaultCatalog(t)
	for _, p := range []string{
		"", "contacts", "/contacts/../auth/me", "/contacts/./x", "/contacts//x", "/contacts?limit=1",
		"/contacts#x", "/contacts/%2e%2e", "https://evil.example/contacts", "/contacts/a b", "/contacts/\x00",
		"/realtime/socket/websocket", "/dashboard-image",
	} {
		if _, err := c.Match("GET", p); err == nil {
			t.Fatalf("matched %q", p)
		}
	}
}

func TestDescribeFitsItsBudget(t *testing.T) {
	c := defaultCatalog(t)
	for _, o := range c.Operations() {
		d := c.Describe(o)
		if d.Method != o.Method || d.Path != o.Path {
			t.Fatalf("%s described as %s %s", o.Key(), d.Method, d.Path)
		}
	}
	d := c.Describe(c.Find("PATCH", "/contacts/{id}"))
	if d.Body == nil || len(d.Params) == 0 {
		t.Fatalf("contact update lost its body or params: %+v", d)
	}
}

func TestEndpointsTakingCredentialsAreBlocked(t *testing.T) {
	c := defaultCatalog(t)
	for key := range credentialOperations {
		m, p, _ := strings.Cut(key, " ")
		if o := c.Find(m, p); o == nil || o.Blocked != credentialReason {
			t.Fatalf("%s should be documented and blocked as taking a credential", key)
		}
	}
	for _, key := range []string{
		"POST /emails/onboarding/app-password/{id}", "PUT /integrations/apollo/{id}/key",
		"PUT /integrations/connections/{id}/signing-key", "PATCH /integrations/clay/{id}",
		"POST /emails/grants/{id}/connect", "PATCH /emails/imports/{id}/rows/{line}",
	} {
		m, p, _ := strings.Cut(key, " ")
		if o := c.Find(m, p); o == nil || o.Blocked != credentialReason {
			t.Fatalf("%s takes a credential in its schema but is not blocked", key)
		}
	}
	if o := c.Find("PATCH", "/contacts/{id}"); o.Blocked != "" {
		t.Fatalf("an ordinary write was blocked: %s", o.Blocked)
	}
}
