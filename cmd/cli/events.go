package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/config"
	"github.com/warmbly/warmbly/internal/cli/iostreams"
)

// `warmbly events tail` is the terminal view of the developer WebSocket: the
// same stream the dashboard runs on, printed as it happens. It is the fastest
// way to see whether an integration is receiving what you think it is, without
// standing up a public webhook URL first.
//
// The socket speaks the Phoenix channel protocol, serializer 1.0.0: every
// frame is [join_ref, ref, topic, event, payload].

func newEventsCmd(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "events <command>",
		Short:   "Watch live events as they happen",
		GroupID: groupDevelop,
		Long: `Subscribe to the workspace's live event stream.

This needs a key with the REALTIME_SUBSCRIBE scope. Sign in again with
` + "`warmbly auth refresh --scopes full`" + ` if the key you have does not carry it.`,
	}
	cmd.AddCommand(newEventsTailCmd(f))
	return cmd
}

func newEventsTailCmd(f *Factory) *cobra.Command {
	var (
		intents  []string
		wsURL    string
		orgID    string
		compact  bool
		maxCount int
	)
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Stream live events to the terminal",
		Long: `Print events as Warmbly publishes them: sends, opens, clicks, replies,
inbox arrivals, campaign state, and the custom events your automations fire.

Filter with --intent, which matches the event type as a case-insensitive
substring, so --intent EMAIL takes EMAIL_SENT, EMAIL_OPENED and EMAIL_RECEIVED.
Intents reduce traffic; they are not a permission boundary, and the key's
scopes still decide what reaches you at all.`,
		Example: `  $ warmbly events tail
  $ warmbly events tail --intent EMAIL --intent CAMPAIGN
  $ warmbly events tail --json | jq 'select(.event_type == "EMAIL_REPLIED")'`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runEventsTail(c.Context(), f, wsURL, orgID, intents, compact, maxCount)
		},
	}
	cmd.Flags().StringArrayVar(&intents, "intent", nil, "Only these event families, for example EMAIL or CAMPAIGN")
	cmd.Flags().StringVar(&wsURL, "url", "", "WebSocket URL, when the instance does not advertise one")
	cmd.Flags().StringVar(&orgID, "org", "", "Organization to subscribe to (default: the signed-in one)")
	cmd.Flags().BoolVar(&compact, "compact", false, "One line per event, even on a terminal")
	cmd.Flags().IntVar(&maxCount, "count", 0, "Stop after this many events")
	return cmd
}

func runEventsTail(ctx context.Context, f *Factory, wsURL, orgID string, intents []string, compact bool, maxCount int) error {
	io := f.IO
	r, err := f.Resolved()
	if err != nil {
		return err
	}

	if orgID == "" && r.Entry != nil {
		orgID = r.Entry.OrganizationID
	}
	if orgID == "" {
		// The key knows its own organization even when the config file does not.
		client, cerr := f.Client()
		if cerr != nil {
			return cerr
		}
		var id struct {
			OrganizationID string `json:"organization_id"`
		}
		if jerr := client.JSON(ctx, api.Request{Method: http.MethodGet, Path: "/me"}, &id); jerr != nil {
			return jerr
		}
		orgID = id.OrganizationID
	}
	if orgID == "" {
		return fmt.Errorf("this credential is not scoped to a workspace, so there is no org channel to join")
	}

	endpoint, err := resolveSocketURL(ctx, f, r, wsURL)
	if err != nil {
		return err
	}

	target := endpoint
	if !strings.Contains(target, "?") {
		target += "?"
	} else {
		target += "&"
	}
	target += "vsn=1.0.0&token=" + url.QueryEscape(r.Token)

	if f.Debug {
		io.Errorf("* connecting to %s\n", endpoint)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, target, nil)
	if resp != nil && resp.Body != nil {
		// The handshake response body carries the rejection reason and nothing
		// the stream needs; the socket itself is what stays open.
		defer resp.Body.Close()
	}
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("the realtime gateway refused this key.\nIt needs the REALTIME_SUBSCRIBE scope: `warmbly auth refresh --scopes full`.")
		}
		return fmt.Errorf("could not connect to the realtime gateway at %s: %w\nPass --url if this instance serves it somewhere else.", endpoint, err)
	}
	defer conn.Close()

	topic := "org:" + orgID
	payload := map[string]any{}
	if len(intents) > 0 {
		payload["intents"] = intents
	}
	if err := conn.WriteJSON([]any{"1", "1", topic, "phx_join", payload}); err != nil {
		return err
	}

	io.Errorf("%s Listening on %s%s\n", io.Gray("…"), io.Bold(topic), intentSuffix(io, intents))
	io.Errorln(io.Gray("Ctrl-C to stop."))

	// Heartbeats are client-initiated. The join reply carries the cadence the
	// server wants; until it arrives, the documented default is safe.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	done := make(chan error, 1)

	go func() {
		seen := 0
		for {
			var frame []json.RawMessage
			if err := conn.ReadJSON(&frame); err != nil {
				done <- err
				return
			}
			if len(frame) < 5 {
				continue
			}
			var event string
			var body json.RawMessage
			_ = json.Unmarshal(frame[3], &event)
			body = frame[4]

			switch event {
			case "phx_reply":
				if hb := handleJoinReply(io, body, heartbeat); hb != nil {
					done <- hb
					return
				}
				continue
			case "phx_close", "phx_error":
				done <- fmt.Errorf("the channel closed. Rejoin with `warmbly events tail`.")
				return
			case "phx_join", "heartbeat":
				continue
			}

			printEvent(f, event, body, compact)
			seen++
			if maxCount > 0 && seen >= maxCount {
				done <- nil
				return
			}
		}
	}()

	ref := 2
	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		case err := <-done:
			if err != nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return err
			}
			return nil
		case <-heartbeat.C:
			ref++
			if err := conn.WriteJSON([]any{nil, fmt.Sprint(ref), "phoenix", "heartbeat", map[string]any{}}); err != nil {
				return err
			}
		}
	}
}

// handleJoinReply reads the HELLO the org channel answers a join with: it
// carries the heartbeat cadence, so the client does not hardcode one, and it
// is where a rejected join is reported.
func handleJoinReply(io *iostreams.IOStreams, body json.RawMessage, heartbeat *time.Ticker) error {
	var reply struct {
		Status   string `json:"status"`
		Response struct {
			Role                string `json:"role"`
			HeartbeatIntervalMS int    `json:"heartbeat_interval_ms"`
			Seq                 int64  `json:"seq"`
			Reason              string `json:"reason"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return nil
	}
	if reply.Status == "error" {
		reason := reply.Response.Reason
		if reason == "" {
			reason = "the gateway refused the join"
		}
		return fmt.Errorf("could not join the channel: %s", reason)
	}
	if reply.Response.HeartbeatIntervalMS > 1000 {
		heartbeat.Reset(time.Duration(reply.Response.HeartbeatIntervalMS) * time.Millisecond)
	}
	if reply.Response.Role != "" {
		io.Errorf("%s\n", io.Gray("Joined as "+reply.Response.Role+"."))
	}
	return nil
}

func printEvent(f *Factory, event string, body json.RawMessage, compact bool) {
	io := f.IO
	if f.JSONOut || !io.IsStdoutTTY() {
		io.Println(strings.TrimSpace(string(body)))
		return
	}

	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	stamp := time.Now().Format("15:04:05")

	io.Printf("%s %s %s\n", io.Gray(stamp), eventColour(io, event), io.Gray(summarize(fields)))
	if compact {
		return
	}
	// The interesting ids, on one indented line, so a terminal stays readable
	// while still carrying enough to look something up.
	var parts []string
	for _, key := range []string{"campaign_id", "contact_id", "email_id", "thread_id", "email_account_id", "name", "entity_type", "action"} {
		if v, ok := fields[key]; ok && v != nil && fmt.Sprint(v) != "" {
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
	}
	if len(parts) > 0 {
		io.Printf("  %s\n", io.Gray(strings.Join(parts, "  ")))
	}
}

func eventColour(io *iostreams.IOStreams, event string) string {
	switch {
	case strings.Contains(event, "FAILED"), strings.Contains(event, "BOUNCE"), strings.Contains(event, "ERROR"):
		return io.Red(event)
	case strings.Contains(event, "REPLIED"), strings.Contains(event, "BOOKED"):
		return io.Green(event)
	case strings.Contains(event, "OPENED"), strings.Contains(event, "CLICKED"):
		return io.Cyan(event)
	default:
		return io.Bold(event)
	}
}

// summarize is the short human tail of an event line: whichever descriptive
// field the event happens to carry.
func summarize(fields map[string]any) string {
	for _, key := range []string{"subject", "email", "to", "contact_email", "campaign_name", "message", "status", "name"} {
		if v, ok := fields[key]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				if len(s) > 70 {
					s = s[:69] + "…"
				}
				return s
			}
		}
	}
	return ""
}

// intentSuffix names the filter in the "listening" line, so a stream that goes
// quiet does not look broken when it is only filtered.
func intentSuffix(io *iostreams.IOStreams, intents []string) string {
	if len(intents) == 0 {
		return ""
	}
	return io.Gray(" (" + strings.Join(intents, ", ") + " only)")
}

// resolveSocketURL finds the realtime gateway: the flag, then what the
// instance advertises on /auth/config, then the layouts the installer writes.
func resolveSocketURL(ctx context.Context, f *Factory, r *config.Resolved, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	client := api.New(r.APIURL, "", UserAgent())
	if f.Debug {
		client.Debug = f.IO.ErrOut
	}
	var cfg struct {
		WebsocketURL string `json:"websocket_url"`
	}
	if err := client.JSON(ctx, api.Request{Method: http.MethodGet, Path: "/auth/config", Anonymous: true}, &cfg); err == nil && cfg.WebsocketURL != "" {
		return cfg.WebsocketURL, nil
	}

	host := config.NormalizeHost(r.Host)
	if host == config.DefaultHost {
		return "wss://realtime." + config.DefaultHost + "/socket/websocket", nil
	}
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		return "ws://" + strings.Split(host, ":")[0] + ":4000/socket/websocket", nil
	}
	// The installer's proxy and Caddy layouts both put it on ws.<host>.
	return "wss://ws." + host + "/socket/websocket", nil
}
