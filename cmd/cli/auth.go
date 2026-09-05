package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/oauth"
	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/config"
	"github.com/warmbly/warmbly/internal/models"
)

func newAuthCmd(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auth <command>",
		Short:   "Sign in, sign out, and see who you are",
		GroupID: groupCore,
		Long: `Sign the CLI in to a Warmbly instance.

Signing in through the browser creates one API key named for this machine. It
appears under Settings > API keys and can be revoked there or with
` + "`warmbly auth logout`" + `. Credentials are written to ` + config.HostsPath() + `
at 0600, and WARMBLY_TOKEN overrides the file without ever being written to it,
which is how CI authenticates with no login step.`,
	}
	cmd.AddCommand(
		newAuthLoginCmd(f),
		newAuthStatusCmd(f),
		newAuthTokenCmd(f),
		newAuthSwitchCmd(f),
		newAuthRefreshCmd(f),
		newAuthLogoutCmd(f),
	)
	return cmd
}

// parseScopes accepts the two presets plus any comma or space separated list
// of scope names, in either case, so `--scopes read_campaigns,READ_CONTACTS`
// works and a typo is named rather than silently dropped.
func parseScopes(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	switch strings.ToLower(raw) {
	case "":
		return models.APIPermFullAccess, nil
	case "full", "full-access", "all":
		return models.APIPermFullAccess, nil
	case "read", "read-only", "readonly":
		return models.APIPermReadOnly, nil
	}
	mask, unknown := oauth.ParseScopes(strings.NewReplacer(",", " ", "+", " ").Replace(raw))
	if len(unknown) > 0 {
		return 0, fmt.Errorf("unknown scope %s.\nRun `warmbly key permissions` for the full list, or use the presets: full, read-only.", strings.Join(unknown, ", "))
	}
	if mask == 0 {
		return 0, fmt.Errorf("--scopes granted nothing. Name at least one scope, or use full or read-only.")
	}
	return mask, nil
}

func newAuthLoginCmd(f *Factory) *cobra.Command {
	var (
		hostname  string
		apiURL    string
		withToken bool
		web       bool
		scopeStr  string
		force     bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to a Warmbly instance",
		Long: `Sign in to a Warmbly instance.

With no flags this asks which instance, then how: a browser approval that
creates a key for this machine, or pasting a key you already have.`,
		Example: `  $ warmbly auth login
  $ warmbly auth login --hostname warmbly.acme.com
  $ warmbly auth login --scopes read-only
  $ echo $KEY | warmbly auth login --with-token`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runAuthLogin(c.Context(), f, hostname, apiURL, withToken, web, scopeStr, force)
		},
	}
	cmd.Flags().StringVar(&hostname, "hostname", "", "Instance to sign in to (default: warmbly.com)")
	cmd.Flags().StringVar(&apiURL, "api-url", "", "API base URL, when it is not derivable from the hostname")
	cmd.Flags().BoolVar(&withToken, "with-token", false, "Read an API key from stdin instead of using the browser")
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Go straight to the browser approval")
	cmd.Flags().StringVarP(&scopeStr, "scopes", "s", "", "Scopes to request: full, read-only, or a list of names")
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing sign-in for this host without asking")
	return cmd
}

func runAuthLogin(ctx context.Context, f *Factory, hostname, apiURL string, withToken, web bool, scopeStr string, force bool) error {
	io := f.IO
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	hosts, err := f.Hosts()
	if err != nil {
		return err
	}

	// 1. Which instance.
	if hostname == "" && apiURL == "" {
		if !io.IsStdinTTY() {
			hostname = config.DefaultHost
		} else {
			idx, serr := io.Select("Where do you want to sign in?", []string{
				"warmbly.com (the hosted service)",
				"A self-hosted instance",
			})
			if serr != nil {
				return serr
			}
			if idx == 0 {
				hostname = config.DefaultHost
			} else {
				answer, ierr := io.Input("Instance hostname (for example warmbly.acme.com)", "")
				if ierr != nil {
					return ierr
				}
				if strings.TrimSpace(answer) == "" {
					return fmt.Errorf("a hostname is required to sign in to a self-hosted instance")
				}
				hostname = answer
			}
		}
	}
	if hostname == "" {
		hostname = apiURL
	}
	host := config.NormalizeHost(hostname)

	if existing := hosts[host]; existing != nil && !force {
		if !io.IsStdinTTY() {
			return fmt.Errorf("already signed in to %s as %s. Pass --force to replace that sign-in.", host, existing.User)
		}
		ok, cerr := io.Confirm(fmt.Sprintf("Already signed in to %s as %s. Sign in again?", host, existing.User), false)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return errCancelled
		}
	}

	// 2. Where its API is.
	base, err := resolveAPIBase(ctx, f, host, apiURL)
	if err != nil {
		return err
	}

	// 3. How to authenticate.
	useToken := withToken
	if !withToken && !web && io.IsStdinTTY() {
		idx, serr := io.Select("How do you want to sign in?", []string{
			"Approve in a browser (creates a key for this machine)",
			"Paste an API key you already have",
		})
		if serr != nil {
			return serr
		}
		useToken = idx == 1
	}

	scopes, err := parseScopes(scopeStr)
	if err != nil {
		return err
	}

	var entry *config.Host
	if useToken {
		entry, err = loginWithToken(ctx, f, base)
	} else {
		entry, err = loginWithBrowser(ctx, f, cfg, base, scopes)
	}
	if err != nil {
		return err
	}

	hosts[host] = entry
	if err := hosts.Save(); err != nil {
		return err
	}
	// The first sign-in, or an explicit one, becomes the active host: nobody
	// expects to sign in and still be pointed somewhere else.
	if cfg.ActiveHost == "" || len(hosts) == 1 || f.HostFlag == "" {
		cfg.ActiveHost = host
		if err := cfg.Save(); err != nil {
			return err
		}
	}

	io.Errorf("%s Signed in to %s as %s\n", io.Tick(), io.Bold(host), io.Bold(entry.User))
	if entry.Organization != "" {
		io.Errorf("  Workspace %s\n", entry.Organization)
	}
	io.Errorf("  Credentials written to %s\n", config.HostsPath())
	return nil
}

// resolveAPIBase finds the API for a host, probing the layouts the installer
// writes. Guessing wrong here produces a confusing 404 on every later command,
// so it is settled once, at sign-in, and stored.
func resolveAPIBase(ctx context.Context, f *Factory, host, explicit string) (string, error) {
	if explicit != "" {
		return strings.TrimRight(explicit, "/"), nil
	}
	if v := strings.TrimSpace(os.Getenv(config.APIURLEnv)); v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	if host == config.DefaultHost {
		return config.DefaultAPIURL(host), nil
	}

	candidates := config.CandidateAPIURLs(host)
	for _, base := range candidates {
		if reachable(ctx, f, base) {
			if f.Debug {
				f.IO.Errorf("* resolved API base %s\n", base)
			}
			return base, nil
		}
	}
	return "", fmt.Errorf("could not find a Warmbly API for %s. Tried:\n  %s\nPass --api-url with the instance's API base URL (the API_PUBLIC_URL it is configured with).", host, strings.Join(candidates, "\n  "))
}

// deploymentConfig is the slice of GET /auth/config the CLI uses: enough to
// tell a Warmbly API from any other 200, plus the two URLs a client cannot
// derive on a self-hosted instance.
type deploymentConfig struct {
	Registration string `json:"registration"`
	AppURL       string `json:"app_url"`
	WebsocketURL string `json:"websocket_url"`
}

// fetchDeploymentConfig reads the public deployment facts, or nil when the
// base is not a Warmbly API. /health alone would accept any 200.
func fetchDeploymentConfig(ctx context.Context, f *Factory, base string) *deploymentConfig {
	client := api.New(base, "", UserAgent())
	client.HTTP.Timeout = 8 * time.Second
	if f.Debug {
		client.Debug = f.IO.ErrOut
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := client.Do(probeCtx, api.Request{Method: http.MethodGet, Path: "/auth/config", Anonymous: true})
	if err != nil || resp == nil || resp.Status != http.StatusOK {
		return nil
	}
	var probe deploymentConfig
	if json.Unmarshal(resp.Body, &probe) != nil || probe.Registration == "" {
		return nil
	}
	return &probe
}

func reachable(ctx context.Context, f *Factory, base string) bool {
	return fetchDeploymentConfig(ctx, f, base) != nil
}

func loginWithToken(ctx context.Context, f *Factory, base string) (*config.Host, error) {
	io := f.IO
	if io.IsStdinTTY() {
		io.Errorln(io.Gray("Create a key under Settings > API keys, then paste it here. It is not echoed."))
	}
	token, err := io.Secret("API key")
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("no key was given")
	}
	if !strings.HasPrefix(token, apikey.KeyPrefix) {
		return nil, fmt.Errorf("that does not look like a Warmbly API key: it should start with %q", apikey.KeyPrefix)
	}

	entry := &config.Host{APIURL: base, Token: token, AddedAt: time.Now().UTC()}
	if err := fillIdentity(ctx, f, entry); err != nil {
		return nil, err
	}
	if cfg := fetchDeploymentConfig(ctx, f, base); cfg != nil {
		entry.AppURL = cfg.AppURL
	}
	return entry, nil
}

func loginWithBrowser(ctx context.Context, f *Factory, cfg *config.Config, base string, scopes uint64) (*config.Host, error) {
	io := f.IO
	client := api.New(base, "", UserAgent())
	if f.Debug {
		client.Debug = io.ErrOut
	}

	machine, _ := os.Hostname()
	start, err := startDeviceFlow(ctx, client, machine, scopes)
	if err != nil {
		return nil, err
	}

	target := start.VerificationURLComplete
	if target == "" {
		target = start.VerificationURL
	}
	io.Errorf("\n  %s %s\n", io.Gray("Your code:"), io.Bold(start.UserCode))
	io.Errorf("  %s %s\n\n", io.Gray("Approve at:"), target)

	if io.IsStdinTTY() {
		if ok, cerr := io.Confirm("Open that in your browser now?", true); cerr == nil && ok {
			if berr := openBrowser(cfg, target); berr != nil {
				io.Errorln(io.Gray("Could not open a browser. Use the link above."))
			}
		}
	}

	io.Errorf("%s Waiting for approval (the code expires in %d minutes)\n", io.Gray("…"), start.ExpiresIn/60)
	result, err := pollDeviceFlow(ctx, client, start)
	if err != nil {
		return nil, err
	}

	entry := &config.Host{
		APIURL:         base,
		AppURL:         appURLFromVerification(start.VerificationURL),
		Token:          result.Token,
		User:           result.UserEmail,
		UserID:         result.UserID,
		Organization:   result.OrganizationName,
		OrganizationID: result.OrganizationID,
		Scopes:         result.ScopeNames,
		APIKeyID:       result.APIKeyID,
		AddedAt:        time.Now().UTC(),
	}
	if entry.User == "" {
		// The approval did not carry an identity; ask the API who we are.
		if err := fillIdentity(ctx, f, entry); err != nil {
			return nil, err
		}
	}
	return entry, nil
}

// appURLFromVerification recovers the dashboard origin from the approval link
// the instance just handed us, which is the instance's own APP_URL and so is
// exact where a hostname guess is not.
func appURLFromVerification(raw string) string {
	return strings.TrimSuffix(strings.TrimSpace(raw), "/cli")
}

// fillIdentity calls /me, which both validates the credential and gives the
// host entry the labels `auth status` prints.
func fillIdentity(ctx context.Context, f *Factory, entry *config.Host) error {
	client := api.New(entry.APIURL, entry.Token, UserAgent())
	if f.Debug {
		client.Debug = f.IO.ErrOut
	}
	var id models.Identity
	if err := client.JSON(ctx, api.Request{Method: http.MethodGet, Path: "/me"}, &id); err != nil {
		if api.StatusOf(err) == http.StatusUnauthorized {
			return fmt.Errorf("that key was rejected by %s. Check it is a key for this instance and has not been revoked.", entry.APIURL)
		}
		return err
	}
	entry.User = id.Email
	entry.UserID = id.UserID.String()
	entry.Scopes = id.Scopes
	entry.Organization = id.OrganizationName
	if id.OrganizationID != nil {
		entry.OrganizationID = id.OrganizationID.String()
	}
	return nil
}

func newAuthStatusCmd(f *Factory) *cobra.Command {
	var showToken bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which hosts you are signed in to",
		Long: `Show every signed-in host, who you are on it, and where the credential
came from. Run this first when a command fails with a credential error: an
environment variable nobody remembers exporting is the usual answer.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runAuthStatus(c.Context(), f, showToken)
		},
	}
	cmd.Flags().BoolVarP(&showToken, "show-token", "t", false, "Print the token itself")
	return cmd
}

func runAuthStatus(ctx context.Context, f *Factory, showToken bool) error {
	io := f.IO
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	hosts, err := f.Hosts()
	if err != nil {
		return err
	}

	resolved, resolveErr := f.Resolved()
	names := hosts.Names()
	// A token from the environment points at a host that may not be in the
	// file at all; it still deserves a line.
	if resolveErr == nil && hosts[resolved.Host] == nil {
		names = append(names, resolved.Host)
	}

	if len(names) == 0 {
		io.Errorf("%s Not signed in anywhere.\n", io.Cross())
		io.Errorln(io.Gray("Run `warmbly auth login` to sign in."))
		return errSilent
	}
	sort.Strings(names)

	failed := false
	for _, name := range names {
		active := resolveErr == nil && name == resolved.Host
		marker := "  "
		if active {
			marker = io.Green("* ")
		}
		io.Printf("%s%s\n", marker, io.Bold(name))

		entry := hosts[name]
		token := ""
		source := config.HostsPath()
		base := ""
		if entry != nil {
			token, base = entry.Token, entry.APIURL
		}
		if active {
			token, source, base = resolved.Token, resolved.Source, resolved.APIURL
		}

		if token == "" {
			io.Printf("    %s no credential\n", io.Cross())
			failed = true
			continue
		}

		probe := &config.Host{APIURL: base, Token: token}
		if err := fillIdentity(ctx, f, probe); err != nil {
			io.Printf("    %s %s\n", io.Cross(), err.Error())
			failed = true
		} else {
			io.Printf("    %s signed in as %s\n", io.Tick(), io.Bold(probe.User))
			if probe.Organization != "" {
				io.Printf("    - workspace: %s\n", probe.Organization)
			}
			io.Printf("    - scopes: %s\n", scopeSummary(probe.Scopes))
		}
		io.Printf("    - api: %s\n", base)
		io.Printf("    - token from: %s\n", source)
		if showToken {
			io.Printf("    - token: %s\n", token)
		} else {
			io.Printf("    - token: %s\n", maskToken(token))
		}
	}

	if cfg.ActiveHost != "" && len(names) > 1 {
		io.Println()
		io.Println(io.Gray("The * host is the one commands use. `warmbly auth switch` changes it."))
	}
	if failed {
		return errSilent
	}
	return nil
}

// scopeSummary keeps a full-access key from printing twenty-four lines.
func scopeSummary(scopes []string) string {
	if len(scopes) == 0 {
		return "none reported"
	}
	if len(scopes) >= len(models.AllAPIPermissions) {
		return fmt.Sprintf("all %d", len(scopes))
	}
	if len(scopes) > 6 {
		return fmt.Sprintf("%s and %d more", strings.Join(scopes[:6], ", "), len(scopes)-6)
	}
	return strings.Join(scopes, ", ")
}

func maskToken(t string) string {
	if len(t) <= 12 {
		return strings.Repeat("*", len(t))
	}
	return t[:8] + strings.Repeat("*", 8) + t[len(t)-4:]
}

func newAuthTokenCmd(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print the token the CLI is using",
		Long: `Print the active token on stdout and nothing else, so it can be piped
into another tool or exported into a CI environment.`,
		Example: `  $ export WARMBLY_TOKEN=$(warmbly auth token)
  $ curl -H "Authorization: Bearer $(warmbly auth token)" https://api.warmbly.com/v1/me`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			r, err := f.Resolved()
			if err != nil {
				return err
			}
			f.IO.Println(r.Token)
			return nil
		},
	}
}

func newAuthSwitchCmd(f *Factory) *cobra.Command {
	var hostname string
	cmd := &cobra.Command{
		Use:   "switch",
		Short: "Change which signed-in host commands use",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 1 {
				hostname = args[0]
			}
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			hosts, err := f.Hosts()
			if err != nil {
				return err
			}
			names := hosts.Names()
			if len(names) == 0 {
				return fmt.Errorf("not signed in anywhere. Run `warmbly auth login` first.")
			}
			if hostname == "" {
				if len(names) == 1 {
					hostname = names[0]
				} else {
					labels := make([]string, len(names))
					for i, n := range names {
						labels[i] = n
						if hosts[n].User != "" {
							labels[i] += "  " + f.IO.Gray(hosts[n].User)
						}
					}
					idx, serr := f.IO.Select("Use which host?", labels)
					if serr != nil {
						return serr
					}
					hostname = names[idx]
				}
			}
			host := config.NormalizeHost(hostname)
			if hosts[host] == nil {
				return fmt.Errorf("not signed in to %s. Signed in to: %s", host, strings.Join(names, ", "))
			}
			cfg.ActiveHost = host
			if err := cfg.Save(); err != nil {
				return err
			}
			f.IO.Errorf("%s Now using %s\n", f.IO.Tick(), f.IO.Bold(host))
			return nil
		},
	}
	cmd.Flags().StringVar(&hostname, "hostname", "", "Host to switch to")
	return cmd
}

func newAuthRefreshCmd(f *Factory) *cobra.Command {
	var scopeStr string
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Sign in again, usually to add scopes",
		Long: `Run the browser sign-in again for the active host.

A key's scopes are fixed when it is created, so widening what the CLI may do
means a new key. The old one is revoked once the new one works.`,
		Example: `  $ warmbly auth refresh --scopes full
  $ warmbly auth refresh --scopes read_campaigns,send_campaigns`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			r, err := f.Resolved()
			if err != nil {
				return err
			}
			old := r.Entry
			if err := runAuthLogin(c.Context(), f, r.Host, r.APIURL, false, true, scopeStr, true); err != nil {
				return err
			}
			// Revoke the key we replaced, so refreshing does not accumulate a
			// key per run under Settings > API keys.
			if old != nil && old.APIKeyID != "" {
				if err := revokeKey(c.Context(), f, old.APIKeyID); err != nil && f.Debug {
					f.IO.Errorf("* could not revoke the previous key: %v\n", err)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&scopeStr, "scopes", "s", "", "Scopes to request: full, read-only, or a list of names")
	return cmd
}

func newAuthLogoutCmd(f *Factory) *cobra.Command {
	var hostname string
	var keepKey bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Sign out and revoke this machine's key",
		Long: `Forget a host's credential.

The key the sign-in created is revoked on the instance too, so signing out on
a machine you are handing back actually ends its access. --keep-key skips the
revocation, for a key you pasted in and use elsewhere.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runAuthLogout(c.Context(), f, hostname, keepKey)
		},
	}
	cmd.Flags().StringVar(&hostname, "hostname", "", "Host to sign out of (default: the active one)")
	cmd.Flags().BoolVar(&keepKey, "keep-key", false, "Forget the credential locally without revoking it")
	return cmd
}

func runAuthLogout(ctx context.Context, f *Factory, hostname string, keepKey bool) error {
	io := f.IO
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	hosts, err := f.Hosts()
	if err != nil {
		return err
	}
	host := config.NormalizeHost(hostname)
	if hostname == "" {
		r, rerr := f.Resolved()
		if rerr != nil {
			return rerr
		}
		host = r.Host
	}
	entry := hosts[host]
	if entry == nil {
		return fmt.Errorf("not signed in to %s", host)
	}

	if !f.AssumeYes && io.IsStdinTTY() {
		ok, cerr := io.Confirm(fmt.Sprintf("Sign out of %s as %s?", host, entry.User), false)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return errCancelled
		}
	}

	// Revoke first: if it fails, the credential is still in the file and the
	// user can retry, which is better than a live key nobody can reach.
	revoked := false
	if !keepKey && entry.APIKeyID != "" {
		f.hosts = hosts
		if err := revokeKeyWith(ctx, f, entry, entry.APIKeyID); err != nil {
			io.Errorf("%s Could not revoke the key on %s: %v\n", io.Yellow("!"), host, err)
			io.Errorln(io.Gray("Revoke it by hand under Settings > API keys."))
		} else {
			revoked = true
		}
	}

	delete(hosts, host)
	if err := hosts.Save(); err != nil {
		return err
	}
	if cfg.ActiveHost == host {
		cfg.ActiveHost = ""
		if names := hosts.Names(); len(names) == 1 {
			cfg.ActiveHost = names[0]
		}
		if err := cfg.Save(); err != nil {
			return err
		}
	}

	io.Errorf("%s Signed out of %s\n", io.Tick(), io.Bold(host))
	if revoked {
		io.Errorln(io.Gray("The key this machine was using has been revoked."))
	}
	return nil
}

func revokeKey(ctx context.Context, f *Factory, keyID string) error {
	r, err := f.Resolved()
	if err != nil {
		return err
	}
	return revokeKeyWith(ctx, f, r.Entry, keyID)
}

// revokeKeyWith ends a key using the credential in entry.
//
// It asks the instance to revoke the calling credential first: self-revocation
// needs no scope, so it works for a read-only sign-in, where deleting by id
// would be refused. The by-id path is the fallback for an instance that
// predates /api-keys/self, and for revoking a key that is not the caller.
func revokeKeyWith(ctx context.Context, f *Factory, entry *config.Host, keyID string) error {
	if entry == nil {
		return fmt.Errorf("no credential to revoke with")
	}
	client := api.New(entry.APIURL, entry.Token, UserAgent())
	if f.Debug {
		client.Debug = f.IO.ErrOut
	}

	if keyID == "" || keyID == entry.APIKeyID {
		_, err := client.Do(ctx, api.Request{Method: http.MethodDelete, Path: "/api-keys/self"})
		if err == nil {
			return nil
		}
		if status := api.StatusOf(err); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			return err
		}
		if keyID == "" {
			return err
		}
	}

	_, err := client.Do(ctx, api.Request{Method: http.MethodDelete, Path: "/api-keys/" + keyID})
	if err != nil && api.StatusOf(err) == http.StatusNotFound {
		// Already gone is the outcome we wanted.
		return nil
	}
	return err
}
