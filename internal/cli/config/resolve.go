package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Resolved is the answer to "who am I and where am I pointed", worked out once
// per invocation from flags, environment and the two files, in that order.
type Resolved struct {
	Host   string
	APIURL string
	Token  string
	// Source says where the token came from, because "it worked yesterday" is
	// almost always an environment variable nobody remembers exporting.
	Source string
	Entry  *Host
}

// ErrNoToken is returned when nothing is signed in. Callers turn it into the
// one message worth printing: how to sign in.
type ErrNoToken struct{ Host string }

func (e *ErrNoToken) Error() string {
	return fmt.Sprintf("not signed in to %s.\nRun `warmbly auth login` to sign in, or set WARMBLY_TOKEN to an API key (wmbly_...).", e.Host)
}

// Resolve works out the active host and token. hostFlag is --host, empty when
// not given.
func Resolve(cfg *Config, hosts Hosts, hostFlag string) (*Resolved, error) {
	host := strings.TrimSpace(hostFlag)
	if host == "" {
		host = strings.TrimSpace(os.Getenv(HostEnv))
	}
	if host == "" {
		host = strings.TrimSpace(cfg.ActiveHost)
	}
	if host == "" {
		// One signed-in host and no preference is not ambiguous.
		if names := hosts.Names(); len(names) == 1 {
			host = names[0]
		}
	}
	if host == "" {
		host = DefaultHost
	}
	host = NormalizeHost(host)

	r := &Resolved{Host: host, Entry: hosts[host]}
	if r.Entry != nil {
		r.APIURL = r.Entry.APIURL
		r.Token = r.Entry.Token
		r.Source = HostsPath()
	}

	// The environment wins, and says so, so a surprising result is traceable.
	if v := strings.TrimSpace(os.Getenv(TokenEnv)); v != "" {
		r.Token, r.Source = v, TokenEnv
	} else if v := strings.TrimSpace(os.Getenv(APIKeyEnv)); v != "" {
		r.Token, r.Source = v, APIKeyEnv
	}
	if v := strings.TrimSpace(os.Getenv(APIURLEnv)); v != "" {
		r.APIURL = strings.TrimRight(v, "/")
	}
	if r.APIURL == "" {
		r.APIURL = DefaultAPIURL(host)
	}
	if r.Token == "" {
		return r, &ErrNoToken{Host: host}
	}
	return r, nil
}

// NormalizeHost turns whatever someone typed into the key hosts.yml uses: a
// bare host, no scheme, no path, no trailing slash.
func NormalizeHost(raw string) string {
	h := strings.TrimSpace(strings.ToLower(raw))
	h = strings.TrimSuffix(h, "/")
	if h == "" {
		return DefaultHost
	}
	if strings.Contains(h, "://") {
		if u, err := url.Parse(h); err == nil && u.Host != "" {
			h = u.Host
		}
	}
	// A bare "app.warmbly.com" or "api.warmbly.com" means the same account as
	// "warmbly.com"; storing three entries for one instance helps nobody. The
	// dot count keeps a real two-label host like "api.dev" intact.
	for _, prefix := range []string{"app.", "api.", "www."} {
		if strings.HasPrefix(h, prefix) && strings.Count(h, ".") > 1 {
			return strings.TrimPrefix(h, prefix)
		}
	}
	return h
}

// DefaultAPIURL is the base URL to try first for a host. The hosted service is
// known; a self-hosted host follows the layout the installer writes.
func DefaultAPIURL(host string) string {
	host = NormalizeHost(host)
	if host == DefaultHost {
		return "https://api." + DefaultHost
	}
	if strings.Contains(host, ":") || strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		return "http://" + host
	}
	return "https://api." + host
}

// CandidateAPIURLs are the bases `auth login` probes for a self-hosted host,
// most likely first. The installer's three shapes produce the first three.
func CandidateAPIURLs(host string) []string {
	host = NormalizeHost(host)
	if strings.Contains(host, "://") {
		return []string{strings.TrimRight(host, "/")}
	}
	scheme := "https"
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		scheme = "http"
	}
	// A host with a port is already an exact address; do not invent subdomains.
	if strings.Contains(host, ":") {
		return []string{scheme + "://" + host}
	}
	return []string{
		scheme + "://api." + host,
		scheme + "://" + host,
		scheme + "://" + host + "/api",
	}
}
