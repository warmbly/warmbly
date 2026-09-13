// Package formserver is the public face of hosted forms: a standalone HTTP
// service (cmd/forms) that serves the TanStack form app (forms/ at the repo
// root), its per-form page shells, the embed loader, and a same-origin JSON
// API that forwards to the backend over the internal API. It exists so
// nothing customer-facing answers on the API origin and form traffic scales
// on its own box. Like the workers and the tracking service, it holds no
// database: the backend owns validation, contacts and counters; this process
// owns the shell (with its per-form CSP), caching and the per-visitor abuse
// checks only it can see.
package formserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/formwire"
)

// formMaxSubmitBytes caps a public submit body; the largest legitimate form
// is dozens of short answers.
const formMaxSubmitBytes = 64 << 10

const (
	submitWindow       = 10 * time.Minute
	submitDefaultLimit = 30
	viewDedupeTTL      = 30 * time.Minute
	maxSourceURLLen    = 2048
	// eventWindowLimit bounds funnel events per source IP per submitWindow; a
	// long multi-page session emits a couple dozen.
	eventWindowLimit = 120
	// formMaxEventBytes caps an event body; it is a handful of short fields.
	formMaxEventBytes = 8 << 10
)

type Config struct {
	// BackendURL and InternalToken reach the backend's /api/v1/internal
	// endpoints, the service's only dependency.
	BackendURL    string
	InternalToken string
	// BrowserPostHogKey and BrowserPostHogHost are stamped into the page shell
	// so the form app can report pageviews, web vitals and browser errors to
	// PostHog. An empty key, which is the default, means the app never loads
	// the SDK. BrowserPostHogErrorTracking false keeps the analytics and
	// reports no exceptions.
	BrowserPostHogKey           string
	BrowserPostHogHost          string
	BrowserPostHogErrorTracking bool
	// BrowserSentryDSN is the same for an operator who reports to Sentry
	// instead. Either backend, both, or neither.
	BrowserSentryDSN string
	// Release tags those browser events with the build serving them.
	Release string
	// Environment is the deployment label those events carry, so staging and
	// production form pages are separable in one project.
	Environment string
	// StaticDir is the built forms app (forms/dist): index.html is the page
	// shell, assets/ the hashed bundles.
	StaticDir string
	// SubmitLimit is submissions per source IP per 10 minutes; 0 means the
	// default of 30.
	SubmitLimit int
}

type Server struct {
	client    *backendClient
	views     *ttlSet
	limits    *ipLimiter
	events    *ipLimiter
	renderKey []byte
	shell     []byte
	assetsDir string
}

func New(cfg Config) (*Server, error) {
	if cfg.BackendURL == "" {
		return nil, errors.New("formserver: BackendURL is required")
	}
	if cfg.InternalToken == "" {
		return nil, errors.New("formserver: InternalToken is required")
	}
	if cfg.StaticDir == "" {
		return nil, errors.New("formserver: StaticDir is required")
	}
	shell, err := os.ReadFile(filepath.Join(cfg.StaticDir, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("formserver: cannot read %s/index.html (run `pnpm build` in forms/): %w", cfg.StaticDir, err)
	}
	// The browser credentials and the release are the same for every request,
	// so they are stamped once here rather than per shell serve. Left empty the
	// placeholders stay empty and the page loads no reporting SDK at all.
	shell = stampMeta(shell, "wf-posthog-key", cfg.BrowserPostHogKey)
	shell = stampMeta(shell, "wf-posthog-host", cfg.BrowserPostHogHost)
	if !cfg.BrowserPostHogErrorTracking {
		shell = stampMeta(shell, "wf-posthog-errors", "false")
	}
	shell = stampMeta(shell, "wf-sentry-dsn", cfg.BrowserSentryDSN)
	shell = stampMeta(shell, "wf-release", cfg.Release)
	shell = stampMeta(shell, "wf-environment", cfg.Environment)
	limit := cfg.SubmitLimit
	if limit <= 0 {
		limit = submitDefaultLimit
	}
	return &Server{
		client:    newBackendClient(strings.TrimRight(cfg.BackendURL, "/"), cfg.InternalToken),
		views:     newTTLSet(viewDedupeTTL),
		limits:    newIPLimiter(limit, submitWindow),
		events:    newIPLimiter(eventWindowLimit, submitWindow),
		renderKey: renderKey(cfg.InternalToken),
		shell:     shell,
		assetsDir: filepath.Join(cfg.StaticDir, "assets"),
	}, nil
}

func (s *Server) Router(trustedProxies []string) (*gin.Engine, error) {
	r := gin.Default()
	// Same posture as the backend: trust no proxy unless the operator names
	// it, so a forged X-Forwarded-For cannot dodge the submit limiter.
	if len(trustedProxies) > 0 {
		if err := r.SetTrustedProxies(trustedProxies); err != nil {
			return nil, err
		}
	} else if err := r.SetTrustedProxies(nil); err != nil {
		return nil, err
	}
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/forms.js", s.ServeFormsEmbedJS)
	r.GET("/f/:publicID", s.ServeFormShell)

	// Hashed filenames, so the bundles are immutable by construction.
	assets := r.Group("/assets", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	})
	assets.Static("/", s.assetsDir)

	// Every JSON call needs the render token minted with the page shell, so
	// scripted clients that never load the page get nothing.
	api := r.Group("/api", s.requireRenderToken)
	api.GET("/forms/:publicID", s.GetPublicForm)
	api.POST("/forms/:publicID/submit", s.SubmitForm)
	api.POST("/forms/:publicID/events", s.RecordFormEvent)
	return r, nil
}

// requireRenderToken rejects API calls whose X-Warmbly-Render token is
// missing, forged, expired or minted for another form. The app treats the
// stale_page error as "refresh this tab".
func (s *Server) requireRenderToken(c *gin.Context) {
	token := c.GetHeader("X-Warmbly-Render")
	if token == "" || !verifyRenderToken(s.renderKey, c.Param("publicID"), token, time.Now()) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "stale_page"})
		return
	}
	c.Next()
}

// fetchForm resolves a form or reports how the lookup ended.
func (s *Server) fetchForm(c *gin.Context) (*formwire.PublicForm, error) {
	publicID := c.Param("publicID")
	if publicID == "" || len(publicID) > 64 {
		return nil, errFormNotFound
	}
	return s.client.Form(c.Request.Context(), publicID)
}

// wfTokenPlaceholder is the meta tag forms/index.html ships; the shell serve
// stamps the real render token into it.
const wfTokenPlaceholder = `<meta name="wf-token" content="" />`

// stampMeta fills in one of the empty meta tags forms/index.html ships. An
// empty value is left alone, and so is a shell built before the tag existed.
func stampMeta(shell []byte, name, value string) []byte {
	if value == "" {
		return shell
	}
	placeholder := []byte(`<meta name="` + name + `" content="" />`)
	stamped := []byte(`<meta name="` + name + `" content="` + html.EscapeString(value) + `" />`)
	return bytes.Replace(shell, placeholder, stamped, 1)
}

// ServeFormShell serves the app shell for a published form. Public and
// unauthenticated: the unguessable public id is the capability. The form is
// resolved before serving so unknown ids 404 like any dead link, the
// per-form embed CSP rides on the document the browser frames, and the shell
// carries the render token the JSON API requires.
func (s *Server) ServeFormShell(c *gin.Context) {
	f, err := s.fetchForm(c)
	if errors.Is(err, errFormNotFound) {
		formNotFoundPage(c)
		return
	}
	if err != nil {
		formUnavailablePage(c)
		return
	}
	setFormFrameHeaders(c, f.AllowedDomains)
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

	token := mintRenderToken(s.renderKey, f.PublicID, time.Now())
	stamped := []byte(`<meta name="wf-token" content="` + token + `" />`)
	shell := bytes.Replace(s.shell, []byte(wfTokenPlaceholder), stamped, 1)
	if bytes.Equal(shell, s.shell) {
		// Older build without the placeholder: inject before </head>.
		shell = bytes.Replace(s.shell, []byte("</head>"), append(stamped, []byte("</head>")...), 1)
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", shell)
}

// isPrefetch reports whether the request is a prefetch or link preview, so
// they never count toward funnel stats.
func isPrefetch(c *gin.Context) bool {
	for _, header := range []string{"Sec-Purpose", "Purpose", "X-Purpose", "X-Moz"} {
		if v := c.GetHeader(header); v != "" && strings.Contains(strings.ToLower(v), "prefetch") {
			return true
		}
	}
	return false
}

// GetPublicForm hands the app the form definition: what the visitor may see
// and nothing more (the embed allowlist stays server-side with the CSP). A
// ?t= ticket bypasses the cache so prefill never leaks across visitors.
func (s *Server) GetPublicForm(c *gin.Context) {
	var f *formwire.PublicForm
	var err error
	if token := c.Query("t"); token != "" && len(token) <= 64 {
		f, err = s.client.FormWithToken(c.Request.Context(), c.Param("publicID"), token)
	} else {
		f, err = s.fetchForm(c)
	}
	if errors.Is(err, errFormNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"public_id":        f.PublicID,
		"name":             f.Name,
		"fields":           f.Fields,
		"design":           f.Design,
		"logo_url":         f.LogoURL,
		"cover_url":        f.CoverURL,
		"background_url":   f.BackgroundURL,
		"captcha_site_key": f.CaptchaSiteKey,
		"prefill":          f.Prefill,
		"link_token":       f.LinkToken,
	})
}

// eventBody is what the app beacons; keep in sync with forms/src/events.ts.
type eventBody struct {
	Type       string `json:"type"`
	PageIndex  int    `json:"page_index"`
	PagesTotal int    `json:"pages_total"`
	VisitorKey string `json:"visitor_key"`
	SourceURL  string `json:"source_url"`
	LinkToken  string `json:"link_token"`
}

// RecordFormEvent accepts a funnel beacon, applies the checks only this
// process can make (prefetch filter, per-IP budget, view dedupe) and
// forwards it for enrichment. Always 204: a beacon never surfaces errors.
func (s *Server) RecordFormEvent(c *gin.Context) {
	if isPrefetch(c) {
		c.Status(http.StatusNoContent)
		return
	}
	ip := c.ClientIP()
	if ip != "" && !s.events.Allow(ip) {
		c.Status(http.StatusNoContent)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, formMaxEventBytes)
	var body eventBody
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	publicID := c.Param("publicID")
	if body.Type == "view" && !s.views.Add(publicID+":"+truncateStr(body.VisitorKey, 64)) {
		c.Status(http.StatusNoContent)
		return
	}
	sourceURL := body.SourceURL
	if sourceURL == "" {
		sourceURL = c.GetHeader("Referer")
	}
	if len(sourceURL) > maxSourceURLLen {
		sourceURL = sourceURL[:maxSourceURLLen]
	}
	go s.client.RecordEvent(publicID, &formwire.EventRequest{
		Type:       body.Type,
		PageIndex:  body.PageIndex,
		PagesTotal: body.PagesTotal,
		VisitorKey: truncateStr(body.VisitorKey, 64),
		SourceURL:  sourceURL,
		LinkToken:  truncateStr(body.LinkToken, 64),
		RemoteIP:   ip,
		UserAgent:  truncateStr(c.GetHeader("User-Agent"), 512),
	})
	c.Status(http.StatusNoContent)
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// submitBody is what the app posts; keep in sync with forms/src/api.ts.
type submitBody struct {
	Answers map[string][]string `json:"answers"`
	// Website is the honeypot value; a human never fills it.
	Website      string `json:"website"`
	WT           int64  `json:"_wt"`
	CaptchaToken string `json:"captcha_token"`
	SourceURL    string `json:"source_url"`
	LinkToken    string `json:"link_token"`
	VisitorKey   string `json:"visitor_key"`
}

// SubmitForm accepts a public submission, runs the checks only this process
// can make (per-IP budget, body cap) and forwards the rest to the backend.
// Same-origin with the app, so there is no CORS surface.
func (s *Server) SubmitForm(c *gin.Context) {
	fail := func(status int, msg string) {
		c.JSON(status, gin.H{"error": "form_submit_failed", "message": msg})
	}

	if ip := c.ClientIP(); ip != "" && !s.limits.Allow(ip) {
		c.Header("Retry-After", fmt.Sprintf("%d", int(submitWindow.Seconds())))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":   "rate_limit_exceeded",
			"message": "Too many submissions from this address. Try again later.",
			"code":    "rate_limit_exceeded",
		})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, formMaxSubmitBytes)
	var body submitBody
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			fail(http.StatusRequestEntityTooLarge, "The submission is too large.")
			return
		}
		fail(http.StatusBadRequest, "The submission could not be read.")
		return
	}

	sourceURL := body.SourceURL
	if sourceURL == "" {
		sourceURL = c.GetHeader("Referer")
	}
	if len(sourceURL) > maxSourceURLLen {
		sourceURL = sourceURL[:maxSourceURLLen]
	}

	req := &formwire.SubmitRequest{
		Answers:        body.Answers,
		RemoteIP:       c.ClientIP(),
		SourceURL:      sourceURL,
		CaptchaToken:   body.CaptchaToken,
		HoneypotFilled: strings.TrimSpace(body.Website) != "",
		RenderedAt:     body.WT,
		LinkToken:      truncateStr(body.LinkToken, 64),
		VisitorKey:     truncateStr(body.VisitorKey, 64),
	}

	out, err := s.client.Submit(c.Request.Context(), c.Param("publicID"), req)
	if err != nil {
		fail(http.StatusBadGateway, "Something went wrong. Please try again.")
		return
	}
	switch {
	case out.NotFound:
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	case out.Rejected != "":
		fail(http.StatusBadRequest, out.Rejected)
		return
	}
	c.JSON(http.StatusOK, out.Result)
}

// ServeFormsEmbedJS serves the embed loader. Cached for an hour, mirroring
// the tracking service's tracking.js.
func (s *Server) ServeFormsEmbedJS(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", formsEmbedJS)
}
