// The forms service: the public face of hosted lead-capture forms. Serves
// /f/<publicID> pages, the /forms.js embed loader and public submissions on
// its own port, so form traffic never touches the API origin. No Postgres,
// no Redis, no event bus: the backend's internal API (BACKEND_INTERNAL_URL +
// INTERNAL_API_TOKEN, the same pair the tracking service uses) is its only
// dependency.
package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/formserver"
	"github.com/warmbly/warmbly/internal/observability"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

func main() {
	// Error reporting, before anything that can fail. Optional here as
	// everywhere: no POSTHOG_KEY and no SENTRY_DSN means nothing is sent
	// anywhere. A failure to configure it must not stop the service serving
	// forms.
	if err := observability.InitEnv("forms"); err != nil {
		log.Printf("error reporting not configured: %v", err)
	}
	defer errs.Flush(2 * time.Second)

	backendURL := strings.TrimSpace(os.Getenv("BACKEND_INTERNAL_URL"))
	if backendURL == "" {
		log.Fatal("BACKEND_INTERNAL_URL is required (the backend's internal API base, e.g. http://localhost:8080)")
	}
	token := os.Getenv("INTERNAL_API_TOKEN")
	if token == "" {
		log.Fatal("INTERNAL_API_TOKEN is required (must match the backend's)")
	}

	submitLimit := 0
	if v := os.Getenv("FORM_IP_RATE_LIMIT"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			log.Fatalf("invalid FORM_IP_RATE_LIMIT %q", v)
		}
		submitLimit = parsed
	}

	staticDir := os.Getenv("FORMS_STATIC_DIR")
	if staticDir == "" {
		// The repo-relative default fits `make forms` (run from the root) and
		// the bare-metal layout (WorkingDirectory=/opt/warmbly).
		staticDir = "forms/dist"
	}

	srv, err := formserver.New(formserver.Config{
		BackendURL:    backendURL,
		InternalToken: token,
		StaticDir:     staticDir,
		SubmitLimit:   submitLimit,
		// The browser half of analytics and error reporting, separate from
		// this process's own credentials: form pages are public and their
		// events belong in a frontend project, not the service's. Empty means
		// the page loads no SDK at all, which is the self-host default.
		BrowserPostHogKey:           strings.TrimSpace(os.Getenv("WARMBLY_POSTHOG_KEY")),
		BrowserPostHogHost:          strings.TrimSpace(os.Getenv("WARMBLY_POSTHOG_HOST")),
		BrowserPostHogErrorTracking: browserPostHogErrorTracking(),
		BrowserSentryDSN:            strings.TrimSpace(os.Getenv("WARMBLY_SENTRY_DSN")),
		Release:                     observability.Release(),
		Environment:                 appEnv(),
	})
	if err != nil {
		log.Fatal(err)
	}

	r, err := srv.Router(splitCSV(os.Getenv("TRUSTED_PROXIES")))
	if err != nil {
		log.Fatalf("invalid TRUSTED_PROXIES: %v", err)
	}

	port := os.Getenv("FORMS_PORT")
	if port == "" {
		port = "8090"
	}
	log.Printf("forms service listening on :%s (backend %s)", port, backendURL)
	if err := r.Run(":" + port); err != nil {
		// The listener dying is the one failure here worth reporting: the
		// boot-time checks above are operator configuration, not a bug.
		errs.CaptureException(err)
		errs.Flush(2 * time.Second)
		log.Fatal(err)
	}
}

// browserPostHogErrorTracking is the WARMBLY_POSTHOG_ERROR_TRACKING switch the
// dashboard and the admin panel read, resolved here because the form page is
// served to a stranger's browser and can only act on what was stamped into
// it. Off keeps the key for the page's analytics and reports no exceptions.
func browserPostHogErrorTracking() bool {
	raw := strings.TrimSpace(os.Getenv("WARMBLY_POSTHOG_ERROR_TRACKING"))
	if raw == "" {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return enabled
}

// appEnv is the deployment label, matching what InitEnv reports for this
// process so the browser and the server halves agree.
func appEnv() string {
	if env := strings.TrimSpace(os.Getenv("APP_ENV")); env != "" {
		return env
	}
	return "dev"
}

func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
