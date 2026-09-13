package formserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/app/form"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// stubFormService backs the REAL internal handlers, so this file tests the
// whole wire: forms service -> HTTP -> backend handler -> service call.
type stubFormService struct {
	form.Service // unimplemented methods panic; the internal endpoints use only these

	mu      sync.Mutex
	form    *models.Form
	answers map[string][]string
	meta    form.SubmitMeta
	events  []form.EventInput
	reject  *errx.Error

	// linkToken maps to linkContact when ResolveLink is asked about it.
	linkToken   string
	linkContact uuid.UUID
	prefill     map[string]string
}

func (s *stubFormService) PublicForm(_ context.Context, publicID string) (*models.Form, *errx.Error) {
	if s.form == nil || publicID != s.form.PublicID {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	return s.form, nil
}

func (s *stubFormService) RecordEvent(_ context.Context, publicID string, in form.EventInput) *errx.Error {
	if s.form == nil || publicID != s.form.PublicID {
		return errx.New(errx.NotFound, "form not found")
	}
	s.mu.Lock()
	s.events = append(s.events, in)
	s.mu.Unlock()
	return nil
}

func (s *stubFormService) ResolveLink(_ context.Context, f *models.Form, token string) (*models.FormLink, map[string]string) {
	if token == "" || token != s.linkToken || f == nil || f.ID != s.form.ID {
		return nil, nil
	}
	return &models.FormLink{ID: uuid.MustParse(token), FormID: f.ID, ContactID: s.linkContact}, s.prefill
}

func (s *stubFormService) Submit(_ context.Context, publicID string, answers map[string][]string, meta form.SubmitMeta) (*form.SubmitResult, *errx.Error) {
	if s.form == nil || publicID != s.form.PublicID {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	if s.reject != nil {
		return nil, s.reject
	}
	s.mu.Lock()
	s.answers, s.meta = answers, meta
	s.mu.Unlock()
	return &form.SubmitResult{Message: "Thanks!"}, nil
}

func (s *stubFormService) eventCount(t string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.Type == t {
			n++
		}
	}
	return n
}

const shellMarker = `<div id="root"></div>`

func writeStaticDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	html := `<!doctype html><html><head><meta name="wf-token" content="" /></head><body>` + shellMarker + `</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newFixture(t *testing.T, submitLimit int) (*stubFormService, *Server, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	stub := &stubFormService{form: &models.Form{
		ID:       uuid.New(),
		PublicID: "pubtest123",
		Name:     "Demo request",
		Fields: []models.FormField{
			{ID: "email", Type: models.FormFieldEmail, Label: "Work email", Required: true, MapTo: "email"},
		},
		AllowedDomains: []string{"example.com"},
	}}
	h := &handler.Handler{FormService: stub}
	backend := gin.New()
	backend.GET("/api/v1/internal/forms/:publicID", h.InternalGetPublicForm)
	backend.POST("/api/v1/internal/forms/:publicID/events", h.InternalRecordFormEvent)
	backend.POST("/api/v1/internal/forms/:publicID/submissions", h.InternalSubmitForm)
	ts := httptest.NewServer(backend)
	t.Cleanup(ts.Close)

	srv, err := New(Config{BackendURL: ts.URL, InternalToken: "test-token", StaticDir: writeStaticDir(t), SubmitLimit: submitLimit})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	r, err := srv.Router(nil)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return stub, srv, r
}

func get(r *gin.Engine, path, renderToken string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if renderToken != "" {
		req.Header.Set("X-Warmbly-Render", renderToken)
	}
	r.ServeHTTP(w, req)
	return w
}

func post(r *gin.Engine, path, renderToken string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if renderToken != "" {
		req.Header.Set("X-Warmbly-Render", renderToken)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func mintFor(srv *Server, publicID string) string {
	return mintRenderToken(srv.renderKey, publicID, time.Now())
}

func TestFormServerServesShellWithRenderToken(t *testing.T) {
	_, _, r := newFixture(t, 0)

	w := get(r, "/f/pubtest123", "")
	if w.Code != http.StatusOK {
		t.Fatalf("shell status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), shellMarker) {
		t.Fatal("shell did not serve the built app")
	}
	if strings.Contains(w.Body.String(), `name="wf-token" content=""`) {
		t.Fatal("render token was not stamped into the shell")
	}
	if !strings.Contains(w.Body.String(), `name="wf-token" content="`) {
		t.Fatal("shell is missing the render token meta")
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "example.com") {
		t.Fatalf("embed allowlist not enforced: %q", csp)
	}
	if w := get(r, "/f/unknownid", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id status %d", w.Code)
	}
}

func TestFormServerRequiresRenderToken(t *testing.T) {
	_, srv, r := newFixture(t, 0)

	if w := get(r, "/api/forms/pubtest123", ""); w.Code != http.StatusForbidden {
		t.Fatalf("missing token status %d, want 403", w.Code)
	}
	if w := get(r, "/api/forms/pubtest123", "garbage"); w.Code != http.StatusForbidden {
		t.Fatalf("forged token status %d, want 403", w.Code)
	}
	// A token minted for another form must not open this one.
	if w := get(r, "/api/forms/pubtest123", mintFor(srv, "otherform")); w.Code != http.StatusForbidden {
		t.Fatalf("cross-form token status %d, want 403", w.Code)
	}
	expired := mintRenderToken(srv.renderKey, "pubtest123", time.Now().Add(-2*renderTokenTTL))
	if w := get(r, "/api/forms/pubtest123", expired); w.Code != http.StatusForbidden {
		t.Fatalf("expired token status %d, want 403", w.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	w := get(r, "/api/forms/pubtest123", "garbage")
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != "stale_page" {
		t.Fatalf("403 body: %s", w.Body.String())
	}
	if w := get(r, "/api/forms/pubtest123", mintFor(srv, "pubtest123")); w.Code != http.StatusOK {
		t.Fatalf("valid token status %d: %s", w.Code, w.Body.String())
	}
}

func TestFormServerServesFormJSONAndAssets(t *testing.T) {
	_, srv, r := newFixture(t, 0)
	token := mintFor(srv, "pubtest123")

	w := get(r, "/api/forms/pubtest123", token)
	if w.Code != http.StatusOK {
		t.Fatalf("form json status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		PublicID string             `json:"public_id"`
		Name     string             `json:"name"`
		Fields   []models.FormField `json:"fields"`
		Allowed  []string           `json:"allowed_domains"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Name != "Demo request" || len(res.Fields) != 1 || res.Fields[0].Label != "Work email" {
		t.Fatalf("form json: %s", w.Body.String())
	}
	if res.Allowed != nil {
		t.Fatal("embed allowlist must not reach the client")
	}
	if w := get(r, "/api/forms/unknownid", mintFor(srv, "unknownid")); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id status %d", w.Code)
	}

	a := get(r, "/assets/index-abc123.js", "")
	if a.Code != http.StatusOK || !strings.Contains(a.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset serving: %d %q", a.Code, a.Header().Get("Cache-Control"))
	}
}

func TestFormServerPrefillPassthrough(t *testing.T) {
	stub, srv, r := newFixture(t, 0)
	stub.linkToken = uuid.NewString()
	stub.linkContact = uuid.New()
	stub.prefill = map[string]string{"email": "jane@example.com"}

	w := get(r, "/api/forms/pubtest123?t="+stub.linkToken, mintFor(srv, "pubtest123"))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Prefill   map[string]string `json:"prefill"`
		LinkToken string            `json:"link_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Prefill["email"] != "jane@example.com" || res.LinkToken != stub.linkToken {
		t.Fatalf("prefill did not cross the wire: %s", w.Body.String())
	}

	// A bogus token degrades to the anonymous payload, never an error.
	w = get(r, "/api/forms/pubtest123?t="+uuid.NewString(), mintFor(srv, "pubtest123"))
	if w.Code != http.StatusOK {
		t.Fatalf("bogus token status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "jane@example.com") {
		t.Fatal("prefill leaked to an unidentified visitor")
	}
}

func TestFormServerRecordsEventsWithViewDedupe(t *testing.T) {
	stub, srv, r := newFixture(t, 0)
	token := mintFor(srv, "pubtest123")

	view := map[string]any{"type": "view", "visitor_key": "vk-1", "pages_total": 2, "source_url": "https://example.com/pricing"}
	if w := post(r, "/api/forms/pubtest123/events", token, view); w.Code != http.StatusNoContent {
		t.Fatalf("event status %d", w.Code)
	}
	// Same visitor again: deduped before it reaches the backend.
	post(r, "/api/forms/pubtest123/events", token, view)
	// Different visitor: counts.
	post(r, "/api/forms/pubtest123/events", token, map[string]any{"type": "view", "visitor_key": "vk-2"})
	// Page progress is never deduped.
	post(r, "/api/forms/pubtest123/events", token, map[string]any{"type": "page", "visitor_key": "vk-1", "page_index": 1, "pages_total": 2})

	deadline := time.Now().Add(2 * time.Second)
	for (stub.eventCount("view") < 2 || stub.eventCount("page") < 1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := stub.eventCount("view"); got != 2 {
		t.Fatalf("view events forwarded = %d, want 2", got)
	}
	if got := stub.eventCount("page"); got != 1 {
		t.Fatalf("page events forwarded = %d, want 1", got)
	}
	// Forwarding is async, so find the view rather than assuming arrival order.
	stub.mu.Lock()
	var view1 *form.EventInput
	for i, e := range stub.events {
		if e.Type == "view" && e.VisitorKey == "vk-1" {
			view1 = &stub.events[i]
			break
		}
	}
	stub.mu.Unlock()
	if view1 == nil {
		t.Fatal("the first view never reached the backend")
	}
	if view1.SourceURL != "https://example.com/pricing" || view1.RemoteIP == "" {
		t.Fatalf("event enrichment inputs lost: %+v", *view1)
	}

	// Prefetches are dropped before the budget or the backend.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/forms/pubtest123/events", strings.NewReader(`{"type":"view","visitor_key":"vk-3"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Warmbly-Render", token)
	req.Header.Set("Sec-Purpose", "prefetch")
	r.ServeHTTP(w, req)
	time.Sleep(50 * time.Millisecond)
	if got := stub.eventCount("view"); got != 2 {
		t.Fatalf("prefetch view was forwarded (count %d)", got)
	}
}

func TestFormServerForwardsSubmission(t *testing.T) {
	stub, srv, r := newFixture(t, 0)
	stub.linkToken = uuid.NewString()
	stub.linkContact = uuid.New()

	w := post(r, "/api/forms/pubtest123/submit", mintFor(srv, "pubtest123"), map[string]any{
		"answers":     map[string][]string{"email": {"visitor@example.com"}},
		"website":     "bot-filled", // honeypot: forwarded as a signal, not an answer
		"_wt":         1700000000,
		"link_token":  stub.linkToken,
		"visitor_key": "vk-9",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("submit status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Message != "Thanks!" {
		t.Fatalf("submit response: %s", w.Body.String())
	}
	if got := stub.answers["email"]; len(got) != 1 || got[0] != "visitor@example.com" {
		t.Fatalf("answers did not cross the wire: %+v", stub.answers)
	}
	if !stub.meta.HoneypotFilled {
		t.Fatal("honeypot signal lost on the wire")
	}
	if stub.meta.RenderedAt.Unix() != 1700000000 {
		t.Fatalf("rendered-at lost on the wire: %v", stub.meta.RenderedAt)
	}
	if stub.meta.LinkToken != stub.linkToken || stub.meta.VisitorKey != "vk-9" {
		t.Fatalf("attribution lost on the wire: %+v", stub.meta)
	}
}

func TestFormServerRelaysRejectionMessage(t *testing.T) {
	stub, srv, r := newFixture(t, 0)
	stub.reject = errx.New(errx.BadRequest, "Email is required.")

	w := post(r, "/api/forms/pubtest123/submit", mintFor(srv, "pubtest123"), map[string]any{
		"answers": map[string][]string{"email": {""}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var res struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Message != "Email is required." {
		t.Fatalf("rejection not relayed verbatim: %s", w.Body.String())
	}
}

func TestFormServerSubmitRateLimit(t *testing.T) {
	_, srv, r := newFixture(t, 2)
	token := mintFor(srv, "pubtest123")
	payload := map[string]any{"answers": map[string][]string{"email": {"a@b.co"}}}
	for i := 0; i < 2; i++ {
		if w := post(r, "/api/forms/pubtest123/submit", token, payload); w.Code != http.StatusOK {
			t.Fatalf("submit %d status %d", i, w.Code)
		}
	}
	w := post(r, "/api/forms/pubtest123/submit", token, payload)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third submit status %d, want 429", w.Code)
	}
}

// The browser credentials are stamped into the shell once at construction, so a
// page a stranger loads either carries the operator's key and DSN or carries
// empty tags and loads no reporting SDK at all. There is no third state.
func TestFormShellStampsBrowserReporting(t *testing.T) {
	shell := []byte(`<!doctype html><html><head>` +
		`<meta name="wf-posthog-key" content="" />` +
		`<meta name="wf-posthog-host" content="" />` +
		`<meta name="wf-posthog-errors" content="" />` +
		`<meta name="wf-sentry-dsn" content="" />` +
		`<meta name="wf-release" content="" />` +
		`</head><body></body></html>`)

	unset := string(stampMeta(stampMeta(shell, "wf-sentry-dsn", ""), "wf-posthog-key", ""))
	for _, name := range []string{"wf-posthog-key", "wf-sentry-dsn"} {
		if !strings.Contains(unset, `<meta name="`+name+`" content="" />`) {
			t.Fatalf("an unset %s should leave the placeholder untouched, got %s", name, unset)
		}
	}

	set := string(stampMeta(stampMeta(stampMeta(stampMeta(stampMeta(shell,
		"wf-posthog-key", "phc_example"),
		"wf-posthog-host", "https://eu.i.posthog.com"),
		"wf-posthog-errors", "false"),
		"wf-sentry-dsn", `https://k@example.invalid/1`),
		"wf-release", "v1.2.3"))
	for _, want := range []string{
		`<meta name="wf-posthog-key" content="phc_example" />`,
		`<meta name="wf-posthog-host" content="https://eu.i.posthog.com" />`,
		`<meta name="wf-posthog-errors" content="false" />`,
		`<meta name="wf-sentry-dsn" content="https://k@example.invalid/1" />`,
		`<meta name="wf-release" content="v1.2.3" />`,
	} {
		if !strings.Contains(set, want) {
			t.Fatalf("%s was not stamped, got %s", want, set)
		}
	}

	// Both values are operator-supplied, so neither may close the tag and
	// inject markup into a page served to the public.
	for _, name := range []string{"wf-posthog-key", "wf-sentry-dsn"} {
		escaped := string(stampMeta(shell, name, `" /><script>alert(1)</script><meta x="`))
		if strings.Contains(escaped, "<script>") {
			t.Fatalf("stamped %s was not escaped: %s", name, escaped)
		}
	}
}
