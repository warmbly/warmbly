package wmail

import (
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/arf"
	"github.com/warmbly/warmbly/internal/pkg/dsn"
)

// bounceReport inspects a freshly synced inbound message and, when it is a
// permanent delivery-status notification for one of our sends, builds the
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
// The event names the send the NDR is about, so the arrival event can carry it: the
// report's own body is the only place that id appears and the consumer cannot
// read a body, but it is what decides whether the report is about a campaign
// send the customer should see or a warmup send they never made.
func (w *WMail) bounceReport(msg *models.EmailMessageData) *models.JobEventInboundBounce {
	from := strings.Join(msg.From, " ")
	if !dsn.Detect(from, msg.Subject, headerFlagValue(msg.Flags, "Content-Type")) {
		return nil
	}

	body := msg.BodyPlain + "\n" + msg.BodyHTML
	report := dsn.Parse(body).WithFailedRecipients(headerFlagValue(msg.Flags, "X-Failed-Recipients"), msg.Subject, body)
	if !report.Permanent {
		return nil
	}

	// Resolve the original outbound Message-ID: the DSN body's returned headers
	// are the reliable source; fall back to the envelope In-Reply-To.
	originalID := report.OriginalMessageID
	if originalID == "" && len(msg.InReplyTo) > 0 {
		originalID = strings.Trim(msg.InReplyTo[len(msg.InReplyTo)-1], "<>")
	}
	if originalID == "" {
		return nil // nothing to resolve the campaign send against
	}

	return &models.JobEventInboundBounce{
		UserID:            w.UserID,
		EmailID:           w.ID,
		OriginalMessageID: strings.Trim(originalID, "<>"),
		FailedRecipient:   report.FailedRecipient,
		Reason:            msg.Subject,
	}
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

// complaintReport builds INBOUND_COMPLAINT when a synced message is an abuse
// feedback report for one of our sends. A complaint never arrives
// synchronously; it comes back as mail, long after the send succeeded.
//
// IMAP and Gmail expose the report's machine-readable parts, so both work.
// Microsoft Graph returns one rendered body and no parts, so a report synced
// through Graph carries only its human notice and is not detected; reading
// those needs a separate MIME fetch, which is not built.
// Like bounceReport, the event names the send the report is about so the
// arrival event can carry it.
func (w *WMail) complaintReport(msg *models.EmailMessageData) *models.JobEventInboundComplaint {
	from := strings.Join(msg.From, " ")
	if !arf.Detect(from, msg.Subject, headerFlagValue(msg.Flags, "Content-Type")) {
		return nil
	}

	report := arf.Parse(msg.BodyPlain + "\n" + msg.BodyHTML)
	if !report.IsComplaint || report.OriginalMessageID == "" {
		return nil
	}

	return &models.JobEventInboundComplaint{
		UserID:              w.UserID,
		EmailID:             w.ID,
		OriginalMessageID:   strings.Trim(report.OriginalMessageID, "<>"),
		ComplainedRecipient: report.ComplainedRecipient,
		Provider:            report.UserAgent,
	}
}
