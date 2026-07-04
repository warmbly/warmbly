package wmail

import (
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/dsn"
)

// maybeEmitBounce inspects a freshly synced inbound message and, when it is a
// permanent delivery-status notification for one of our sends, emits an
// INBOUND_BOUNCE event so the consumer can suppress the recipient and record the
// bounce against the campaign. This is where API-sent (Gmail/Graph) mail finally
// gets bounce tracking: those sends succeed synchronously, so the only bounce
// signal is the NDR that lands back in the mailbox.
//
// Runs on the worker because the full DSN body is in hand here (the consumer has
// no S3 access). It only PARSES — resolution and suppression stay control-plane.
// Best-effort and permanent-only: a message that doesn't parse to a permanent
// failure with a resolvable original id is silently ignored, so a transient
// (4.x.x) bounce never suppresses a valid recipient.
func (w *WMail) maybeEmitBounce(msg *models.EmailMessageData) {
	from := strings.Join(msg.From, " ")
	if !dsn.Detect(from, msg.Subject, headerFlagValue(msg.Flags, "Content-Type")) {
		return
	}

	report := dsn.Parse(msg.BodyPlain + "\n" + msg.BodyHTML)
	if !report.Permanent {
		return
	}

	// Resolve the original outbound Message-ID: the DSN body's returned headers
	// are the reliable source; fall back to the envelope In-Reply-To.
	originalID := report.OriginalMessageID
	if originalID == "" && len(msg.InReplyTo) > 0 {
		originalID = strings.Trim(msg.InReplyTo[len(msg.InReplyTo)-1], "<>")
	}
	if originalID == "" {
		return // nothing to resolve the campaign send against
	}

	_ = w.onEvent(models.JobEventTypeInboundBounce, &models.JobEventInboundBounce{
		UserID:            w.UserID,
		EmailID:           w.ID,
		OriginalMessageID: strings.Trim(originalID, "<>"),
		FailedRecipient:   report.FailedRecipient,
		Reason:            msg.Subject,
	})
}

// headerFlagValue reads a "Header:value" pseudo-flag out of the flag slice (the
// sidecar the sync mappers use to carry internet headers). Returns "" if absent.
func headerFlagValue(flags []string, name string) string {
	prefix := name + ":"
	for _, f := range flags {
		if strings.HasPrefix(f, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(f, prefix))
		}
	}
	return ""
}
