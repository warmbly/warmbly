package config

import (
	"os"
	"strings"
	"time"
)

const (
	// SyncPushFallbackDefault is how often a mailbox whose provider pushes its
	// changes is still polled, because push notifications can be delayed or lost.
	SyncPushFallbackDefault = 10 * time.Minute
	// SyncImapWatchedInterval is the poll for an IMAP mailbox whose INBOX is held
	// in IDLE: only the other folders, such as spam, still wait for it.
	SyncImapWatchedInterval = 3 * time.Minute
	// SyncWakeMinGap merges a burst of change notifications into one sync pass.
	SyncWakeMinGap = 2 * time.Second
	// SyncWakeCoalesce is how long the control plane drops repeat wakes for one mailbox.
	SyncWakeCoalesce = 2 * time.Second
)

// ImapIdleEnabled reports whether workers hold each IMAP INBOX in IDLE. On
// unless IMAP_IDLE is set to false.
func ImapIdleEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("IMAP_IDLE")))
	return v != "false" && v != "0" && v != "off"
}

// SyncPushFallbackInterval is SYNC_PUSH_FALLBACK_INTERVAL, bounded to 2-60 minutes.
func SyncPushFallbackInterval() time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(os.Getenv("SYNC_PUSH_FALLBACK_INTERVAL")))
	if err != nil || d <= 0 {
		return SyncPushFallbackDefault
	}
	return min(max(d, 2*time.Minute), time.Hour)
}

// GraphPushEnabled reports whether Outlook mailboxes subscribe to Graph change
// notifications. It needs a backend reachable by Microsoft over HTTPS.
func GraphPushEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GRAPH_PUSH")))
	return (v == "true" || v == "1" || v == "on") && strings.HasPrefix(BackendPublicURL(), "https://")
}

// GmailPushTopic is the Pub/Sub topic Gmail mailboxes watch into
// ("projects/<project>/topics/<topic>"); empty keeps Gmail polling.
func GmailPushTopic() string {
	return strings.TrimSpace(os.Getenv("GMAIL_PUSH_TOPIC"))
}

// GmailPushServiceAccount is the service account a Pub/Sub push subscription
// signs its requests to the backend with; set, the backend accepts Gmail push.
func GmailPushServiceAccount() string {
	return strings.TrimSpace(os.Getenv("GMAIL_PUSH_SERVICE_ACCOUNT"))
}

// GmailPushSubscription is the pull subscription on GmailPushTopic the consumer
// reads, for an instance Pub/Sub cannot push to.
func GmailPushSubscription() string {
	return strings.TrimSpace(os.Getenv("GMAIL_PUSH_SUBSCRIPTION"))
}
