package instancesettings

import (
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

// Operator notification channels. These are instance-wide, not per
// organization: they answer "tell me when something happens on my deployment",
// which is why they live in the settings document rather than in the
// customer-facing webhook tables.

// Channel transports.
const (
	ChannelDiscord = "discord"
	ChannelSlack   = "slack"
	ChannelWebhook = "webhook"
	ChannelEmail   = "email"
)

// MaxChannels bounds the fan-out. Each delivery is an outbound request the
// backend makes on the instance's behalf, so the list is not unbounded.
const MaxChannels = 25

// Masked is what a secret-bearing field reads as on the way out. A client that
// sends it back unchanged means "keep the stored value".
const Masked = "••••••••"

// NotifyChannel is one operator destination.
type NotifyChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type is one of the Channel* constants.
	Type string `json:"type"`
	// Target is the incoming-webhook URL, or the address for an email channel.
	// It is a bearer credential for every webhook transport (anyone holding a
	// Discord webhook URL can post to that room), so it is masked on read.
	Target string `json:"target"`
	// Secret signs the generic webhook payload (HMAC-SHA256, same scheme as
	// customer webhooks). Ignored by the other transports.
	Secret string `json:"secret,omitempty"`
	// Events subscribed to, by key. Empty means every event.
	Events  []string `json:"events"`
	Enabled bool     `json:"enabled"`
}

// Notifications is the operator notification section.
type Notifications struct {
	Channels []NotifyChannel `json:"channels"`
}

// Wants reports whether this channel should receive the given event.
func (c NotifyChannel) Wants(eventKey string) bool {
	if !c.Enabled {
		return false
	}
	if len(c.Events) == 0 {
		return true
	}
	for _, e := range c.Events {
		if e == eventKey {
			return true
		}
	}
	return false
}

// IsWebhookTransport reports whether Target is a URL rather than an address.
func (c NotifyChannel) IsWebhookTransport() bool {
	return c.Type == ChannelDiscord || c.Type == ChannelSlack || c.Type == ChannelWebhook
}

// Redacted is the channel as it leaves the process: the target reduced to a
// recognisable preview and the secret replaced by a set/unset flag, so an
// admin can tell channels apart without the response handing out credentials.
func (c NotifyChannel) Redacted() NotifyChannel {
	out := c
	out.Target = previewTarget(c.Type, c.Target)
	if c.Secret != "" {
		out.Secret = Masked
	}
	return out
}

// previewTarget keeps an email address readable (it is not a credential) and
// reduces a webhook URL to its host plus the tail of its path.
func previewTarget(kind, target string) string {
	target = strings.TrimSpace(target)
	if target == "" || kind == ChannelEmail {
		return target
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return Masked
	}
	tail := u.Path
	if len(tail) > 6 {
		tail = "…" + tail[len(tail)-6:]
	}
	return u.Host + tail
}

// NormalizeChannels validates, de-duplicates and bounds the channel list.
// Invalid entries are dropped rather than rejected so one bad row saved by an
// older client cannot make the whole document unreadable.
func (n *Notifications) Normalize() {
	seen := make(map[string]bool, len(n.Channels))
	out := make([]NotifyChannel, 0, len(n.Channels))
	for _, ch := range n.Channels {
		ch.Name = strings.TrimSpace(ch.Name)
		ch.Target = strings.TrimSpace(ch.Target)
		ch.Secret = strings.TrimSpace(ch.Secret)
		ch.Type = strings.ToLower(strings.TrimSpace(ch.Type))

		if !validChannelType(ch.Type) || ch.Target == "" {
			continue
		}
		if ch.IsWebhookTransport() && ValidateChannelURL(ch.Target) != nil {
			continue
		}
		if ch.Type == ChannelEmail && !looksLikeEmail(ch.Target) {
			continue
		}
		if ch.ID == "" {
			ch.ID = uuid.NewString()
		}
		if seen[ch.ID] {
			continue
		}
		seen[ch.ID] = true
		if ch.Name == "" {
			ch.Name = defaultChannelName(ch.Type)
		}
		ch.Events = dedupe(ch.Events)
		out = append(out, ch)
		if len(out) >= MaxChannels {
			break
		}
	}
	n.Channels = out
}

func validChannelType(t string) bool {
	switch t {
	case ChannelDiscord, ChannelSlack, ChannelWebhook, ChannelEmail:
		return true
	}
	return false
}

func defaultChannelName(t string) string {
	switch t {
	case ChannelDiscord:
		return "Discord"
	case ChannelSlack:
		return "Slack"
	case ChannelEmail:
		return "Email"
	default:
		return "Webhook"
	}
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func looksLikeEmail(v string) bool {
	at := strings.IndexByte(v, '@')
	return at > 0 && at < len(v)-1 && strings.Contains(v[at+1:], ".") && !strings.ContainsAny(v, " \t\r\n")
}

// ValidateChannelURL applies the same SSRF posture as customer webhooks:
// HTTPS to a publicly routable host, no inline credentials. A self-hosted or
// development deployment opts out with WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS,
// which is how an operator points a channel at a LAN chat server.
func ValidateChannelURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errChannelURL("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errChannelURL("invalid url")
	}
	allowUnsafe := strings.EqualFold(os.Getenv("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS"), "true")
	if u.Scheme != "https" && !(allowUnsafe && u.Scheme == "http") {
		return errChannelURL("url scheme must be https")
	}
	if u.Host == "" {
		return errChannelURL("url must have a host")
	}
	if u.User != nil {
		return errChannelURL("url must not contain credentials")
	}
	if !allowUnsafe && isPrivateHost(u.Hostname()) {
		return errChannelURL("url host must be publicly routable")
	}
	return nil
}

func isPrivateHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return safehttp.IsBlockedIP(ip)
}

type channelURLError string

func (e channelURLError) Error() string { return string(e) }

func errChannelURL(msg string) error { return channelURLError(msg) }

// mergeChannels applies an incoming list over the stored one. The admin panel
// reads channels with their target and secret redacted, so an unchanged field
// comes back as the mask (or empty) and must resolve to what is already
// stored; otherwise saving an unrelated toggle would wipe every credential.
func mergeChannels(stored, incoming []NotifyChannel) []NotifyChannel {
	byID := make(map[string]NotifyChannel, len(stored))
	for _, ch := range stored {
		byID[ch.ID] = ch
	}
	out := make([]NotifyChannel, 0, len(incoming))
	for _, ch := range incoming {
		prev, existed := byID[ch.ID]
		if existed {
			// Only the redacted form means "unchanged". An empty field is a
			// deliberate clear: the panel empties the target when the channel
			// type changes, and restoring the old one there would post Slack
			// payloads to a Discord webhook. An emptied target fails
			// validation in Normalize, which is the intended outcome.
			if ch.Target == Masked || ch.Target == prev.Redacted().Target {
				ch.Target = prev.Target
			}
			// Same for the signing secret, so it can actually be removed.
			if ch.Secret == Masked {
				ch.Secret = prev.Secret
			}
			// A type change invalidates a target carried over from the old
			// transport even when the string was sent back unchanged.
			if ch.Type != prev.Type {
				ch.Target = strings.TrimSpace(ch.Target)
				if ch.Target == Masked || ch.Target == prev.Redacted().Target || ch.Target == prev.Target {
					ch.Target = ""
				}
			}
		}
		out = append(out, ch)
	}
	return out
}

// RedactedChannels is the channel list as it leaves the process.
func (n Notifications) RedactedChannels() []NotifyChannel {
	out := make([]NotifyChannel, 0, len(n.Channels))
	for _, ch := range n.Channels {
		out = append(out, ch.Redacted())
	}
	return out
}

// Subscribers returns every enabled channel that wants the given event.
func (n Notifications) Subscribers(eventKey string) []NotifyChannel {
	out := make([]NotifyChannel, 0, len(n.Channels))
	for _, ch := range n.Channels {
		if ch.Wants(eventKey) {
			out = append(out, ch)
		}
	}
	return out
}

// Find returns the channel with the given id.
func (n Notifications) Find(id string) (NotifyChannel, bool) {
	for _, ch := range n.Channels {
		if ch.ID == id {
			return ch, true
		}
	}
	return NotifyChannel{}, false
}
