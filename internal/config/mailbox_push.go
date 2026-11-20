package config

import (
	"os"
	"strings"
	"time"
)

const (
	// SyncNarrowPollInterval is how often a mailbox with no live change signal gets the narrow check a wake would run.
	SyncNarrowPollInterval = 5 * time.Minute
	// SyncPushSafetyInterval is the narrow check behind a live push channel, which can stop delivering without lapsing.
	SyncPushSafetyInterval = 15 * time.Minute
	// SyncCatchUpInterval spaces full passes while an import or recovery that only the full pass advances is unfinished.
	SyncCatchUpInterval = time.Minute
	// SyncFullCheckDefault is a worker's full check interval when the control plane sends none.
	SyncFullCheckDefault = time.Duration(SyncFullCheckMinMinutesDefault) * time.Minute
	// SyncFullCheckFloor is the shortest full check interval a worker accepts.
	SyncFullCheckFloor = time.Minute
	// SyncFullCheckFastSeconds is the full check interval of placement seeds and pool-linked mailboxes.
	SyncFullCheckFastSeconds = 60
	// SyncFullCheckRecent is the longest two complete full checks are apart at any configured interval, jitter included.
	SyncFullCheckRecent = time.Duration(SyncFullCheckMinutesCeiling+SyncFullCheckMinutesCeiling/10) * time.Minute
	// SyncWakeSettle lets the rest of a burst of change notifications land before a wake's check.
	SyncWakeSettle = 2 * time.Second
	// SyncWakeMinGap is the least time between the starts of two checks of one mailbox; a wake inside it waits.
	SyncWakeMinGap = 30 * time.Second
	// SyncWakeCoalesce is how long the control plane drops repeat wakes for one mailbox. It must not
	// exceed SyncWakeSettle, or a change announced in the dropped window can land after the check.
	SyncWakeCoalesce = SyncWakeSettle
)

// ImapIdleEnabled reports whether workers hold each IMAP INBOX in IDLE. On
// unless IMAP_IDLE is set to false.
func ImapIdleEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("IMAP_IDLE")))
	return v != "false" && v != "0" && v != "off"
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
