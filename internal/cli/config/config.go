// Package config is where the `warmbly` CLI remembers who you are.
//
// Two files, the split gh made conventional: config.yml holds preferences and
// aliases and is safe to read; hosts.yml holds one credential per host and is
// written 0600. Environment variables override both and are never written
// back, so CI sets WARMBLY_TOKEN and never runs a login.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultHost is the hosted service. A bare `warmbly auth login` means this.
const DefaultHost = "warmbly.com"

// Env vars, all documented. TokenEnv is checked first; APIKeyEnv is the name
// warmblyctl already taught people and keeps working.
const (
	DirEnv    = "WARMBLY_CONFIG_DIR"
	TokenEnv  = "WARMBLY_TOKEN"
	APIKeyEnv = "WARMBLY_API_KEY"
	HostEnv   = "WARMBLY_HOST"
	APIURLEnv = "WARMBLY_API_URL"
)

// Host is one signed-in instance.
type Host struct {
	// APIURL is the base the CLI calls, without a trailing slash and without
	// the /v1 prefix. Resolved once at login so no later command has to guess.
	APIURL string `yaml:"api_url"`
	// AppURL is the dashboard origin, as the instance reports it. Stored at
	// sign-in so `warmbly browse` opens the right page rather than guessing
	// from the hostname, which is wrong on any non-default layout.
	AppURL string `yaml:"app_url,omitempty"`
	Token  string `yaml:"token,omitempty"`

	User           string   `yaml:"user,omitempty"`
	UserID         string   `yaml:"user_id,omitempty"`
	Organization   string   `yaml:"organization,omitempty"`
	OrganizationID string   `yaml:"organization_id,omitempty"`
	Scopes         []string `yaml:"scopes,omitempty"`
	// APIKeyID is what `warmbly auth logout` revokes.
	APIKeyID string    `yaml:"api_key_id,omitempty"`
	AddedAt  time.Time `yaml:"added_at,omitempty"`
}

// Config is the preference file.
type Config struct {
	// ActiveHost is what commands use when no --host is given.
	ActiveHost string `yaml:"active_host,omitempty"`
	// Output is the default renderer: table or json.
	Output string `yaml:"output,omitempty"`
	// Confirm is "always" or "sends". "sends" (the default) prompts only for
	// commands that put real mail on the wire.
	Confirm string `yaml:"confirm,omitempty"`
	// Pager is the command long output is piped through, or "cat" to disable.
	Pager string `yaml:"pager,omitempty"`
	// Browser overrides the command used to open a URL.
	Browser string            `yaml:"browser,omitempty"`
	Aliases map[string]string `yaml:"aliases,omitempty"`
}

// Keys are the settable config fields, with what each one does. `warmbly
// config set` refuses anything not in here so a typo is not silently stored.
var Keys = []struct {
	Name, Help, Default string
}{
	{"active_host", "Which signed-in host commands use by default", DefaultHost},
	{"output", "Default output format: table or json", "table"},
	{"confirm", "When to prompt before a command that sends: sends or always", "sends"},
	{"pager", "Program to page long output through; cat disables paging", "$PAGER"},
	{"browser", "Program used to open a URL", "$BROWSER"},
}

// Dir is where both files live: WARMBLY_CONFIG_DIR, then XDG, then ~/.config.
func Dir() string {
	if v := strings.TrimSpace(os.Getenv(DirEnv)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
		return filepath.Join(v, "warmbly")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".warmbly"
	}
	return filepath.Join(home, ".config", "warmbly")
}

func configPath() string { return filepath.Join(Dir(), "config.yml") }
func hostsPath() string  { return filepath.Join(Dir(), "hosts.yml") }

// Load reads config.yml. A missing file is an empty config, not an error: the
// CLI has to work on a machine that has never run it.
func Load() (*Config, error) {
	c := &Config{}
	raw, err := os.ReadFile(configPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return c, fmt.Errorf("reading %s: %w", configPath(), err)
	}
	if err := yaml.Unmarshal(raw, c); err != nil {
		return c, fmt.Errorf("%s is not valid YAML: %w", configPath(), err)
	}
	return c, nil
}

func (c *Config) Save() error {
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return writeFile(configPath(), raw, 0o600)
}

// Get reads one settable key, falling back to its default.
func (c *Config) Get(key string) string {
	switch key {
	case "active_host":
		return c.ActiveHost
	case "output":
		if c.Output == "" {
			return "table"
		}
		return c.Output
	case "confirm":
		if c.Confirm == "" {
			return "sends"
		}
		return c.Confirm
	case "pager":
		return c.Pager
	case "browser":
		return c.Browser
	}
	return ""
}

// Set writes one settable key. Values are validated here rather than at use,
// so a bad value is rejected while the user is still looking at it.
func (c *Config) Set(key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "active_host":
		c.ActiveHost = value
	case "output":
		if value != "table" && value != "json" {
			return fmt.Errorf("output must be table or json, not %q", value)
		}
		c.Output = value
	case "confirm":
		if value != "sends" && value != "always" {
			return fmt.Errorf("confirm must be sends or always, not %q", value)
		}
		c.Confirm = value
	case "pager":
		c.Pager = value
	case "browser":
		c.Browser = value
	default:
		return fmt.Errorf("unknown config key %q. Run `warmbly config list` for the settable keys.", key)
	}
	return nil
}

// Hosts is hosts.yml: host name to credential.
type Hosts map[string]*Host

func LoadHosts() (Hosts, error) {
	h := Hosts{}
	raw, err := os.ReadFile(hostsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return h, nil
		}
		return h, fmt.Errorf("reading %s: %w", hostsPath(), err)
	}
	if err := yaml.Unmarshal(raw, &h); err != nil {
		return h, fmt.Errorf("%s is not valid YAML: %w", hostsPath(), err)
	}
	for name, entry := range h {
		if entry == nil {
			delete(h, name)
		}
	}
	return h, nil
}

// SaveHosts writes the credential file at 0600. An empty map removes the file
// rather than leaving a stub, so `auth logout` leaves nothing behind.
func (h Hosts) Save() error {
	if len(h) == 0 {
		if err := os.Remove(hostsPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	raw, err := yaml.Marshal(map[string]*Host(h))
	if err != nil {
		return err
	}
	return writeFile(hostsPath(), raw, 0o600)
}

// Names returns the configured hosts in a stable order.
func (h Hosts) Names() []string {
	out := make([]string, 0, len(h))
	for name := range h {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	// Write-then-rename so an interrupted save cannot truncate a working file
	// and lock someone out of their own CLI.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return os.Chmod(path, mode)
}

// HostsPath and ConfigPath are exported for `auth status`, which tells people
// where their credentials actually live.
func HostsPath() string  { return hostsPath() }
func ConfigPath() string { return configPath() }
