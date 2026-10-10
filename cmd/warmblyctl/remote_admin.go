package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

type remoteAdminRoute struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Permissions []string `json:"permissions"`
	FreshAuth   bool     `json:"fresh_auth"`
	Blocked     string   `json:"unavailable_reason,omitempty"`
}

type remoteAdminHelper struct {
	Command string
	Method  string
	Path    string
	IDFlag  string
}

var remoteAdminHelpers = []remoteAdminHelper{
	{"workers list", "GET", "/admin/workers", ""},
	{"workers show", "GET", "/admin/workers/:id", "id"},
	{"workers stats", "GET", "/admin/workers/:id/stats", "id"},
	{"workers mailboxes", "GET", "/admin/workers/:id/emails", "id"},
	{"fleet nodes", "GET", "/admin/fleet/nodes", ""},
	{"fleet capacity", "GET", "/admin/fleet/capacity", ""},
	{"fleet decisions", "GET", "/admin/fleet/decisions", ""},
	{"fleet dedicated", "GET", "/admin/fleet/dedicated", ""},
	{"fleet release", "GET", "/admin/fleet/release", ""},
	{"logs", "GET", "/admin/fleet/nodes/:id/logs", "node-id"},
	{"broker", "GET", "/admin/fleet/nodes/:id/broker", "node-id"},
	{"mailbox list", "GET", "/admin/mailboxes", ""},
	{"sync list", "GET", "/admin/sync", ""},
	{"sends in-flight", "GET", "/admin/sends/in-flight", ""},
	{"recovery dead-letters", "GET", "/admin/tasks/dead-letters", ""},
	{"recovery failures", "GET", "/admin/tasks/failures", ""},
	{"recovery replay", "POST", "/admin/tasks/dead-letters/:id/replay", "id"},
	{"recovery clear-throttle", "POST", "/admin/sync/:id/clear-throttle", "id"},
	{"recovery restart-backfill", "POST", "/admin/sync/:id/restart-backfill", "id"},
	{"recovery webhooks", "GET", "/admin/webhooks/health", ""},
	{"recovery reclaim-webhooks", "POST", "/admin/webhooks/reclaim", ""},
	{"jobs list", "GET", "/admin/jobs", ""},
	{"health status", "GET", "/admin/system/status", ""},
	{"health instance", "GET", "/admin/instance/health", ""},
	{"health monitoring", "GET", "/admin/instance/monitoring", ""},
	{"health limits", "GET", "/admin/instance/limits", ""},
}

func remoteAdminUsage() {
	fmt.Fprintln(os.Stderr, "Usage: warmblyctl admin <family> <action> [--url URL | --server NAME] [flags]\nRead-only diagnostics are the default; every mutation requires --write and exact --confirm.")
	for _, helper := range remoteAdminHelpers {
		fmt.Fprintf(os.Stderr, "  %-28s %-6s %s\n", helper.Command, helper.Method, helper.Path)
	}
	fmt.Fprintln(os.Stderr, "  catalog                      Offline JSON inventory of all existing admin routes and gates\n  api METHOD /admin/path       Bounded JSON escape hatch; --query key=value, --data @file or -\nUse --help after a helper to see its typed filters. No raw logs, tokens, message bodies or headers are printed.")
}

func runRemoteAdmin(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		remoteAdminUsage()
		return nil
	}
	if args[0] == "catalog" {
		fs := newRemoteFlagSet("admin catalog")
		if err := parseRemoteFlags(fs, args[1:]); err != nil {
			return err
		}
		routes := append([]remoteAdminRoute(nil), remoteAdminRoutes...)
		sort.Slice(routes, func(i, j int) bool { return routes[i].Path+routes[i].Method < routes[j].Path+routes[j].Method })
		type entry struct {
			remoteAdminRoute
			ReadOnly             bool `json:"read_only"`
			ConfirmationRequired bool `json:"confirmation_required"`
			ReplayRiskRequired   bool `json:"replay_risk_required"`
		}
		entries := make([]entry, 0, len(routes))
		for _, route := range routes {
			entries = append(entries, entry{route, route.Method == "GET", route.Method != "GET", strings.HasSuffix(route.Path, "/replay")})
		}
		return writeRemoteJSON(os.Stdout, entries)
	}
	if args[0] == "api" {
		return runRemoteAdminAPI(ctx, args[1:])
	}
	for _, helper := range remoteAdminHelpers {
		parts := strings.Fields(helper.Command)
		if len(args) < len(parts) {
			continue
		}
		if strings.Join(args[:len(parts)], " ") == helper.Command {
			return runRemoteAdminHelper(ctx, helper, args[len(parts):])
		}
	}
	remoteAdminUsage()
	return remoteFailure(2, "unknown_admin_command", "Unknown admin family or action. Choose a listed helper or catalog entry.")
}

var remotePathWord = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)
var remoteQueryKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func findRemoteAdminRoute(method, requestPath string) (remoteAdminRoute, error) {
	if path.Clean(requestPath) != requestPath || !strings.HasPrefix(requestPath, "/admin/") || strings.ContainsAny(requestPath, "%?#\\\r\n") {
		return remoteAdminRoute{}, remoteFailure(2, "invalid_admin_path", "Use an exact /admin catalog path without query, escapes, traversal or trailing slash. Supply query flags separately.")
	}
	parts := strings.Split(requestPath, "/")
	for _, route := range remoteAdminRoutes {
		if route.Method != method {
			continue
		}
		template := strings.Split(route.Path, "/")
		if len(template) != len(parts) {
			continue
		}
		match := true
		for i, segment := range template {
			if !strings.HasPrefix(segment, ":") {
				if segment != parts[i] {
					match = false
					break
				}
				continue
			}
			name := strings.TrimPrefix(segment, ":")
			if name == "id" || strings.HasSuffix(strings.ToLower(name), "id") {
				id, err := uuid.Parse(parts[i])
				if err != nil || id == uuid.Nil || id.String() != parts[i] {
					match = false
					break
				}
			} else if !remotePathWord.MatchString(parts[i]) || parts[i] == "." || parts[i] == ".." {
				match = false
				break
			}
		}
		if match {
			return route, nil
		}
	}
	return remoteAdminRoute{}, remoteFailure(2, "endpoint_not_cataloged", "Method and path are not a valid catalog entry. No request was sent.")
}

type remoteQueries []string

func (q *remoteQueries) String() string { return "" }
func (q *remoteQueries) Set(value string) error {
	*q = append(*q, value)
	return nil
}

func parseRemoteQueries(raw remoteQueries) (url.Values, error) {
	values := url.Values{}
	if len(raw) > 40 {
		return nil, remoteFailure(2, "invalid_query", "At most 40 query filters are accepted.")
	}
	for _, item := range raw {
		key, value, found := strings.Cut(item, "=")
		if !found || !remoteQueryKey.MatchString(key) || len(value) > 2048 || strings.IndexFunc(value, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return nil, remoteFailure(2, "invalid_query", "Query filters must be bounded key=value pairs without control characters.")
		}
		for _, secret := range []string{"token", "secret", "password", "credential", "authorization", "api_key"} {
			if strings.Contains(key, secret) {
				return nil, remoteFailure(2, "invalid_query", "Credentials and passwords must not be placed in query flags.")
			}
		}
		if _, duplicate := values[key]; duplicate {
			return nil, remoteFailure(2, "invalid_query", "Duplicate query keys are not accepted.")
		}
		values.Set(key, value)
	}
	return values, nil
}

func readRemoteBody(source string) (json.RawMessage, error) {
	if source == "" {
		return nil, nil
	}
	var reader io.Reader = os.Stdin
	if source != "-" {
		if !strings.HasPrefix(source, "@") || len(source) < 2 {
			return nil, remoteFailure(2, "invalid_body_source", "Use --data @file or --data - for stdin. Inline JSON, passwords and credentials are never accepted in argv.")
		}
		f, err := os.Open(strings.TrimPrefix(source, "@"))
		if err != nil {
			return nil, remoteFailure(2, "invalid_body_source", "Cannot open the JSON input file. No filename or raw input was printed.")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, remoteFailure(2, "invalid_body_source", "JSON input must be a regular file or explicit stdin.")
		}
		reader = f
	}
	body, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, remoteFailure(2, "invalid_json", "JSON input is unreadable or exceeds 1 MiB.")
	}
	value, err := decodeRemoteJSON(body)
	if err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, remoteFailure(2, "invalid_json", "Admin JSON input must be an object.")
	}
	return body, nil
}

func executeRemoteAdmin(ctx context.Context, sel remoteSelection, method, requestPath string, query url.Values, body json.RawMessage, write bool, confirmation string, replayRisk bool) error {
	route, err := findRemoteAdminRoute(method, requestPath)
	if err != nil {
		return err
	}
	if route.Blocked != "" {
		return remoteFailure(8, "endpoint_unavailable", route.Blocked+". No request was sent.")
	}
	if method == "GET" {
		if body != nil || write || confirmation != "" || replayRisk {
			return remoteFailure(2, "invalid_arguments", "Read-only commands do not accept bodies or write/replay flags.")
		}
	} else {
		if err := requireRemoteWrite(method, requestPath, write, confirmation); err != nil {
			return err
		}
		if strings.HasSuffix(route.Path, "/replay") && !replayRisk {
			return remoteFailure(8, "replay_risk_required", "Replay also requires --acknowledge-send-risk after inspecting provider evidence. This flag is not evidence that a send did or did not happen. No request was sent.")
		}
	}
	client, err := newRemoteClient(ctx, sel)
	if err != nil {
		return err
	}
	defer client.store.close()
	if len(query) > 0 {
		requestPath += "?" + query.Encode()
	}
	payload, err := client.do(ctx, method, requestPath, body)
	if err != nil {
		return err
	}
	return printRemoteResponse(payload)
}

func runRemoteAdminAPI(ctx context.Context, args []string) error {
	if len(args) < 2 {
		remoteAdminUsage()
		return remoteFailure(2, "invalid_arguments", "admin api requires METHOD /admin/path followed by flags.")
	}
	method, requestPath := strings.ToUpper(args[0]), args[1]
	fs := newRemoteFlagSet("admin api METHOD /admin/path")
	var sel remoteSelection
	sel.flags(fs)
	write, confirm := remoteWriteFlags(fs)
	risk := fs.Bool("acknowledge-send-risk", false, "Explicit dead-letter replay risk acknowledgement; never establishes send authority")
	data := fs.String("data", "", "JSON object from @file or - (stdin), never inline argv")
	var queries remoteQueries
	fs.Var(&queries, "query", "Bounded query filter key=value; may be repeated")
	if err := parseRemoteFlags(fs, args[2:]); err != nil {
		return err
	}
	// Validate route and confirmation before opening body input or credential storage.
	route, err := findRemoteAdminRoute(method, requestPath)
	if err != nil {
		return err
	}
	if route.Blocked != "" {
		return remoteFailure(8, "endpoint_unavailable", route.Blocked)
	}
	if method != "GET" {
		if err := requireRemoteWrite(method, requestPath, *write, *confirm); err != nil {
			return err
		}
	} else if *data != "" {
		return remoteFailure(2, "invalid_arguments", "GET commands cannot carry JSON bodies.")
	}
	query, err := parseRemoteQueries(queries)
	if err != nil {
		return err
	}
	body, err := readRemoteBody(*data)
	if err != nil {
		return err
	}
	return executeRemoteAdmin(ctx, sel, method, requestPath, query, body, *write, *confirm, *risk)
}

func runRemoteAdminHelper(ctx context.Context, helper remoteAdminHelper, args []string) error {
	fs := newRemoteFlagSet("admin " + helper.Command)
	var sel remoteSelection
	sel.flags(fs)
	id := ""
	if helper.IDFlag != "" {
		fs.StringVar(&id, helper.IDFlag, "", "Required canonical UUID for this target")
	}
	query := url.Values{}
	var limit *int
	var cursor, q, state, status, level, after, before, mailbox *string
	if helper.Method == "GET" && (helper.Command == "logs" || helper.Command == "sync list" || helper.Command == "mailbox list" || helper.Command == "sends in-flight" || helper.Command == "recovery dead-letters" || helper.Command == "recovery failures") {
		limit = fs.Int("limit", 100, "Maximum rows, 1 to 200 (server may apply a lower endpoint cap)")
	}
	switch helper.Command {
	case "logs":
		level = fs.String("level", "", "info, warn or error")
		after = fs.String("after", "", "Inclusive RFC3339 observation time")
		before = fs.String("before", "", "Inclusive RFC3339 observation time")
		mailbox = fs.String("mailbox-id", "", "Optional mailbox UUID")
	case "sync list":
		state = fs.String("state", "", "all, throttled, backfilling, stalled, pending or complete")
		q = fs.String("q", "", "Mailbox search (never a password or token)")
		cursor = fs.String("cursor", "", "Cursor from the preceding page")
	case "mailbox list":
		q = fs.String("q", "", "Mailbox search (never a password or token)")
		cursor = fs.String("cursor", "", "Cursor from the preceding page")
	case "recovery dead-letters":
		status = fs.String("status", "", "pending, replayed or failed")
		cursor = fs.String("cursor", "", "Cursor UUID from the preceding page")
	}
	var write bool
	var confirm string
	var risk bool
	if helper.Method != "GET" {
		fs.BoolVar(&write, "write", false, "Explicitly authorize this mutation")
		fs.StringVar(&confirm, "confirm", "", "Repeat exact METHOD /admin/target-path")
		if helper.Command == "recovery replay" {
			fs.BoolVar(&risk, "acknowledge-send-risk", false, "Acknowledge replay risk after inspecting provider evidence")
		}
	}
	if err := parseRemoteFlags(fs, args); err != nil {
		return err
	}
	requestPath := helper.Path
	if helper.IDFlag != "" {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil || parsed.String() != id {
			return remoteFailure(2, "invalid_id", "This helper requires a nonzero canonical UUID target.")
		}
		requestPath = strings.ReplaceAll(requestPath, ":id", id)
	}
	if limit != nil {
		if *limit < 1 || *limit > 200 {
			return remoteFailure(2, "invalid_limit", "--limit must be between 1 and 200.")
		}
		query.Set("limit", strconv.Itoa(*limit))
	}
	for key, filter := range map[string]*string{"q": q, "cursor": cursor, "state": state, "status": status, "level": level, "after": after, "before": before, "mailbox_id": mailbox} {
		if filter != nil && *filter != "" {
			query.Set(key, *filter)
		}
	}
	for key, allowed := range map[string]string{"state": "all throttled backfilling stalled pending complete", "status": "pending replayed failed", "level": "info warn error"} {
		if value := query.Get(key); value != "" && !remoteWordSet(allowed)[value] {
			return remoteFailure(2, "invalid_filter", "Invalid enumerated filter. Use --help for supported values.")
		}
	}
	if value := query.Get("mailbox_id"); value != "" {
		if parsed, err := uuid.Parse(value); err != nil || parsed == uuid.Nil || parsed.String() != value {
			return remoteFailure(2, "invalid_filter", "--mailbox-id requires a canonical UUID.")
		}
	}
	var afterTime, beforeTime time.Time
	for key, target := range map[string]*time.Time{"after": &afterTime, "before": &beforeTime} {
		if value := query.Get(key); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return remoteFailure(2, "invalid_time", "Log timestamps must use RFC3339.")
			}
			*target = parsed
		}
	}
	if !afterTime.IsZero() && !beforeTime.IsZero() && afterTime.After(beforeTime) {
		return remoteFailure(2, "invalid_time", "--after must not be later than --before.")
	}
	if cursor != nil && *cursor != "" {
		if helper.Command == "recovery dead-letters" {
			if parsed, err := uuid.Parse(*cursor); err != nil || parsed.String() != *cursor {
				return remoteFailure(2, "invalid_cursor", "Dead-letter cursor must be a canonical UUID.")
			}
		} else if _, err := paging.DecodeOffsetCursor(*cursor); err != nil {
			return remoteFailure(2, "invalid_cursor", "Use the next_cursor from this endpoint, not a page number.")
		}
	}
	var raw remoteQueries
	for key, values := range query {
		raw = append(raw, key+"="+values[0])
	}
	validated, err := parseRemoteQueries(raw)
	if err != nil {
		return err
	}
	return executeRemoteAdmin(ctx, sel, helper.Method, requestPath, validated, nil, write, confirm, risk)
}
