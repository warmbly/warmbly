package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/app/instancecheck"
	"github.com/warmbly/warmbly/internal/app/instanceconfig"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/notify"
)

// docsBase mirrors DOCS_BASE in admin/src/lib/docs.ts: a check carries a site
// relative path, so the CLI is the one that turns it into something clickable.
const docsBase = "https://docs.warmbly.com"

// transportTimeout bounds building the mail transport. It dials nothing, but it
// still reads configuration that can sit behind a network call.
const transportTimeout = 5 * time.Second

// messageWidth is where a check message is wrapped. Messages are paragraphs,
// and an unwrapped paragraph is what makes a health report unreadable.
const messageWidth = 76

// runChecks runs the same registry the admin panel runs. The Input is empty on
// purpose: a check that needs the live request (Host, Origin, X-Forwarded-For)
// then skips itself rather than failing on something a CLI cannot evaluate.
func runChecks(ctx context.Context, c *conn) ([]instancecheck.Finding, instancecheck.Summary) {
	deps := instancecheck.Deps{
		Runtime:   checkRuntime(),
		Transport: checkTransport(ctx),
	}
	if c != nil && c.db != nil {
		deps.DB = c.db.Pool
	}
	if cc := checkCache(ctx); cc != nil {
		defer cc.Close()
		deps.Cache = cc
	}
	deps.Policy = deps.Runtime.Policy

	return instancecheck.New(deps).Run(ctx, instancecheck.Input{})
}

// checkRuntime is the subset of the backend's boot facts a CLI can honestly
// reconstruct. Every other field stays zero, so the check needing it skips.
func checkRuntime() *instanceconfig.Runtime {
	transport := config.MailTransport()
	delivers := transport != config.MailTransportLog

	return &instanceconfig.Runtime{
		CORSOrigins:       splitEnvList("CORS_ALLOW_ORIGINS"),
		MailTransportKind: transport,
		MailDelivers:      delivers,
		Policy:            config.LoadAuthPolicy(delivers),
		WebsocketURL:      strings.TrimSpace(os.Getenv("WEBSOCKET_URL")),
	}
}

// checkTransport builds the mail sender only for smtp, the one kind with a
// preflight worth running. The identity is read from the environment because
// preflight never sends, so a missing EMAIL_ADDRESS must not skip the relay check.
func checkTransport(ctx context.Context) *notify.Transport {
	if config.MailTransport() != config.MailTransportSMTP {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, transportTimeout)
	defer cancel()

	cfg, err := config.NewConfig(ctx)
	if err != nil {
		return nil
	}
	transport, err := notify.NewTransport(ctx, cfg, os.Getenv("EMAIL_NAME"), os.Getenv("EMAIL_ADDRESS"))
	if err != nil {
		return nil
	}
	return transport
}

// checkCache hands over a client that has not dialled yet, so an unreachable
// Redis reaches the registry as a finding instead of disappearing as a nil dep.
func checkCache(ctx context.Context) *cache.Cache {
	endpoint, err := redisEndpoint(ctx)
	if err != nil {
		return nil
	}
	opts, perr := redis.ParseURL(endpoint)
	if perr != nil {
		return nil
	}
	return &cache.Cache{Client: redis.NewClient(opts)}
}

var checkGroups = []struct {
	severity instancecheck.Severity
	label    string
}{
	{instancecheck.SeverityError, "Errors"},
	{instancecheck.SeverityWarning, "Warnings"},
	{instancecheck.SeverityInfo, "Notes"},
}

// printChecks renders the findings grouped by severity, most severe first, each
// with the message and the page that explains it.
func printChecks(findings []instancecheck.Finding, summary instancecheck.Summary, quiet bool) {
	// Nothing to report means no output at all under --quiet, so a cron entry
	// only mails when something fires.
	if quiet && len(findings) == 0 {
		return
	}

	fmt.Println("Checks")
	if len(findings) == 0 {
		fmt.Println("  All clear. Every check passed, so nothing here needs fixing.")
		return
	}
	fmt.Printf("  %s\n", summaryLine(summary))

	for _, group := range checkGroups {
		labelled := false
		for _, f := range findings {
			if f.Severity != group.severity {
				continue
			}
			if !labelled {
				fmt.Printf("\n  %s\n", group.label)
				labelled = true
			}
			printFinding(f)
		}
	}
}

func printFinding(f instancecheck.Finding) {
	title := f.Title
	if f.Target != "" {
		title = fmt.Sprintf("%s (%s)", f.Title, f.Target)
	}

	fmt.Printf("    %s\n", title)
	for _, line := range wrapText(f.Message, messageWidth) {
		fmt.Printf("      %s\n", line)
	}
	if url := docsURL(f.Docs); url != "" {
		fmt.Printf("      %s\n", url)
	}
}

func summaryLine(s instancecheck.Summary) string {
	return strings.Join([]string{
		countLabel(s.Error, "error", "errors"),
		countLabel(s.Warning, "warning", "warnings"),
		countLabel(s.Info, "note", "notes"),
	}, ", ")
}

func countLabel(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// docsURL resolves a check's docs path against the documentation site, the same
// way the admin panel does.
func docsURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return docsBase + path
}

func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	lines := []string{}
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	return append(lines, line)
}

func splitEnvList(key string) []string {
	out := []string{}
	for _, part := range strings.Split(os.Getenv(key), ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
