package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/iostreams"
	"github.com/warmbly/warmbly/internal/models"
)

// `warmbly status` is the one screen answer to "what is happening in my
// workspace right now". It is several calls composed into one view, and every
// section degrades on its own: a key without analytics scope still gets the
// mailbox and inbox lines rather than one failure for the whole command.

func newStatusCmd(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "What is happening in your workspace right now",
		GroupID: groupCore,
		Long: `A single screen: who you are, which mailboxes need attention, what is
sending today, and what is waiting for a reply.

Sections you have no scope for are skipped rather than failing the command.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runStatus(c.Context(), f)
		},
	}
}

func runStatus(ctx context.Context, f *Factory) error {
	io := f.IO
	client, err := f.Client()
	if err != nil {
		return err
	}

	// --json is one document rather than a rendered screen, so a script gets
	// the same information without parsing prose.
	bundle := map[string]any{}
	collect := func(key, path string, query url.Values) json.RawMessage {
		resp, derr := client.Do(ctx, api.Request{Method: http.MethodGet, Path: path, Query: query})
		if derr != nil {
			if f.Debug {
				io.Errorf("* %s: %v\n", path, derr)
			}
			return nil
		}
		var parsed any
		if json.Unmarshal(resp.Body, &parsed) == nil {
			bundle[key] = parsed
		}
		return resp.Body
	}

	identity := collect("me", "/me", nil)
	mailboxes := collect("mailboxes", "/emails", url.Values{"limit": []string{"100"}})
	campaigns := collect("campaigns", "/campaigns", url.Values{"limit": []string{"100"}})
	unread := collect("inbox", "/unibox/count", nil)
	dashboard := collect("analytics", "/analytics/dashboard", nil)

	if f.JSONOut || !io.IsStdoutTTY() {
		raw, merr := json.MarshalIndent(bundle, "", "  ")
		if merr != nil {
			return merr
		}
		io.Println(string(raw))
		return nil
	}

	printIdentity(io, identity)
	printMailboxes(io, mailboxes)
	printCampaigns(io, campaigns)
	printInbox(io, unread, dashboard)

	if len(bundle) == 0 {
		return fmt.Errorf("nothing could be read with this credential. Run `warmbly auth status` to check it.")
	}
	return nil
}

func printIdentity(io *iostreams.IOStreams, raw json.RawMessage) {
	if raw == nil {
		return
	}
	var id struct {
		Email            string `json:"email"`
		OrganizationName string `json:"organization_name"`
	}
	if json.Unmarshal(raw, &id) != nil {
		return
	}
	line := io.Bold(id.Email)
	if id.OrganizationName != "" {
		line += io.Gray("  in ") + io.Bold(id.OrganizationName)
	}
	io.Printf("%s\n\n", line)
}

func printMailboxes(io *iostreams.IOStreams, raw json.RawMessage) {
	if raw == nil {
		return
	}
	var list struct {
		Data []struct {
			Email         string  `json:"email"`
			Status        string  `json:"status"`
			AuthState     string  `json:"auth_state"`
			CampaignLimit int     `json:"campaign_limit"`
			Warmup        *string `json:"warmup"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return
	}

	total, warming, capacity, unchecked := len(list.Data), 0, 0, 0
	var trouble []string
	for _, m := range list.Data {
		capacity += m.CampaignLimit
		if m.Warmup != nil {
			warming++
		}
		// "unknown" means never checked and never gates sending, so it is not
		// a problem to report; only a failing check is.
		switch {
		case m.Status != "" && m.Status != "active":
			trouble = append(trouble, fmt.Sprintf("%s %s", m.Email, io.Red(m.Status)))
		case m.AuthState == models.AuthStateFailing:
			trouble = append(trouble, fmt.Sprintf("%s %s", m.Email, io.Yellow("SPF, DKIM or DMARC failing")))
		case m.AuthState == models.AuthStateUnknown:
			unchecked++
		}
	}

	io.Printf("%s\n", io.Gray("MAILBOXES"))
	if total == 0 {
		io.Printf("  none connected. `warmbly browse mailboxes` opens the dashboard.\n\n")
		return
	}
	io.Printf("  %d connected, %d warming, %d emails/day of campaign capacity\n", total, warming, capacity)
	for _, t := range trouble {
		io.Printf("  %s %s\n", io.Cross(), t)
	}
	if len(trouble) == 0 {
		io.Printf("  %s all healthy\n", io.Tick())
	}
	if unchecked > 0 {
		io.Printf("  %s\n", io.Gray(fmt.Sprintf("%d never had their authentication checked: `warmbly mailbox recheck <id>`", unchecked)))
	}
	io.Println()
}

func printCampaigns(io *iostreams.IOStreams, raw json.RawMessage) {
	if raw == nil {
		return
	}
	var list struct {
		Data []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return
	}

	counts := map[string]int{}
	var active []string
	for _, c := range list.Data {
		counts[c.Status]++
		if c.Status == "active" || c.Status == "running" {
			active = append(active, c.Name)
		}
	}

	io.Printf("%s\n", io.Gray("CAMPAIGNS"))
	if len(list.Data) == 0 {
		io.Printf("  none yet. `warmbly campaign create --name \"My campaign\"` starts one.\n\n")
		return
	}
	var parts []string
	for _, status := range []string{"active", "paused", "draft", "completed"} {
		if n := counts[status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, status))
		}
	}
	io.Printf("  %s\n", strings.Join(parts, ", "))
	for i, name := range active {
		if i == 5 {
			io.Printf("  %s\n", io.Gray(fmt.Sprintf("and %d more sending", len(active)-5)))
			break
		}
		io.Printf("  %s %s\n", io.Green("▸"), name)
	}
	io.Println()
}

func printInbox(io *iostreams.IOStreams, unread, dashboard json.RawMessage) {
	io.Printf("%s\n", io.Gray("INBOX"))
	if unread != nil {
		var count struct {
			Count int `json:"count"`
			Total int `json:"total"`
		}
		if json.Unmarshal(unread, &count) == nil {
			n := count.Count
			if n == 0 {
				n = count.Total
			}
			if n > 0 {
				io.Printf("  %s unread\n", io.Bold(fmt.Sprint(n)))
			} else {
				io.Printf("  %s nothing unread\n", io.Tick())
			}
		}
	}
	if dashboard != nil {
		var stats map[string]any
		if json.Unmarshal(dashboard, &stats) == nil {
			var parts []string
			for _, key := range []string{"sent", "opened", "clicked", "replied", "bounced"} {
				if v, ok := stats[key]; ok {
					parts = append(parts, fmt.Sprintf("%v %s", v, key))
				}
			}
			if len(parts) > 0 {
				io.Printf("\n%s\n  %s\n", io.Gray("RECENT ACTIVITY"), strings.Join(parts, ", "))
			}
		}
	}
	io.Println()
}
