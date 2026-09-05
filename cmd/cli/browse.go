package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/config"
)

// browseTargets maps a noun to its dashboard path, so `warmbly browse
// campaigns` does not require anyone to remember the URL layout.
var browseTargets = map[string]string{
	"campaigns":      "/app/campaigns",
	"campaign":       "/app/campaigns",
	"contacts":       "/app/contacts",
	"contact":        "/app/contacts",
	"mailboxes":      "/app/emails",
	"mailbox":        "/app/emails",
	"emails":         "/app/emails",
	"inbox":          "/app/unibox",
	"unibox":         "/app/unibox",
	"analytics":      "/app/analytics",
	"automations":    "/app/automations",
	"forms":          "/app/forms",
	"templates":      "/app/templates",
	"crm":            "/app/crm",
	"audit":          "/app/audit",
	"keys":           "/app/api-keys",
	"api-keys":       "/app/api-keys",
	"settings":       "/app/settings/profile",
	"webhooks":       "/app/settings/webhooks",
	"billing":        "/app/settings/billing",
	"members":        "/app/settings/members",
	"deliverability": "/app/deliverability",
}

// browseDetail is the subset that has a per-record page, for `warmbly browse
// campaign <id>`.
var browseDetail = map[string]string{
	"campaign":   "/app/campaigns",
	"campaigns":  "/app/campaigns",
	"contact":    "/app/contacts",
	"contacts":   "/app/contacts",
	"mailbox":    "/app/emails",
	"mailboxes":  "/app/emails",
	"automation": "/app/automations",
	"form":       "/app/forms",
	"forms":      "/app/forms",
}

func newBrowseCmd(f *Factory) *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:     "browse [<section>] [<id>]",
		Short:   "Open the dashboard in a browser",
		GroupID: groupCore,
		Long: `Open the Warmbly dashboard for the host you are signed in to.

With a section it goes straight there; with a section and an id it opens that
record. --no-browser prints the URL instead, which is what you want over SSH.`,
		Example: `  $ warmbly browse
  $ warmbly browse campaigns
  $ warmbly browse campaign 6f1c...
  $ warmbly browse inbox --no-browser`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			r, err := f.Resolved()
			if err != nil {
				return err
			}

			path := "/app/emails"
			if len(args) > 0 {
				section := strings.ToLower(args[0])
				if len(args) > 1 {
					detail, ok := browseDetail[section]
					if !ok {
						return fmt.Errorf("%q has no detail page to open by id. Sections with one: %s", args[0], keysOf(browseDetail))
					}
					path = detail + "/" + args[1]
				} else {
					target, ok := browseTargets[section]
					if !ok {
						return fmt.Errorf("nothing to browse called %q. Try one of: %s", args[0], keysOf(browseTargets))
					}
					path = target
				}
			}

			url := dashboardURL(r) + path
			if printOnly || !f.IO.IsStdoutTTY() {
				f.IO.Println(url)
				return nil
			}
			f.IO.Errorf("%s Opening %s\n", f.IO.Gray("→"), url)
			if err := openBrowser(cfg, url); err != nil {
				f.IO.Println(url)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&printOnly, "no-browser", false, "Print the URL instead of opening it")
	return cmd
}

// dashboardURL is where this host's dashboard lives. The instance reports its
// own APP_URL at sign-in, which is exact; the derivation below is the fallback
// for a credential that came from the environment and never signed in.
func dashboardURL(r *config.Resolved) string {
	if r.Entry != nil && strings.TrimSpace(r.Entry.AppURL) != "" {
		return strings.TrimRight(r.Entry.AppURL, "/")
	}
	return appBaseURL(r.Host)
}

// appBaseURL is the dashboard for a host, following the layout the installer
// writes: app.<host> for a real deployment, the host itself for a local one.
func appBaseURL(host string) string {
	host = config.NormalizeHost(host)
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		return "http://" + host
	}
	if strings.Contains(host, ":") {
		return "http://" + host
	}
	return "https://app." + host
}

// keysOf lists a target map's names, sorted, for an error message.
func keysOf(m map[string]string) string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
