package wmail

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
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
		Reason:            report.Reason(msg.Subject),
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
// IMAP and Gmail expose the report's machine-readable parts in the synced body.
// Microsoft Graph syncs one rendered body, so a report that reads as nothing
// there is parsed again from the message's raw MIME.
// Like bounceReport, the event names the send the report is about so the
// arrival event can carry it.
func (w *WMail) complaintReport(ctx context.Context, msg *models.EmailMessageData) (*models.JobEventInboundComplaint, error) {
	from := strings.Join(msg.From, " ")
	if !arf.Detect(from, msg.Subject, headerFlagValue(msg.Flags, "Content-Type")) {
		return nil, nil
	}

	report := arf.Parse(msg.BodyPlain + "\n" + msg.BodyHTML)
	if !report.IsComplaint || report.OriginalMessageID == "" {
		var err error
		report, err = w.graphComplaintMIME(ctx, msg, report)
		if err != nil {
			return nil, err
		}
	}
	if !report.IsComplaint || report.OriginalMessageID == "" {
		return nil, nil
	}

	return &models.JobEventInboundComplaint{
		UserID:              w.UserID,
		EmailID:             w.ID,
		OriginalMessageID:   strings.Trim(report.OriginalMessageID, "<>"),
		ComplainedRecipient: report.ComplainedRecipient,
		Provider:            report.UserAgent,
	}, nil
}

// complaintMIMEBytes bounds the raw fetch; a report returns the original send, which is small.
const complaintMIMEBytes = 1 << 20

// A temporary raw-fetch failure must hold admission so the provider re-offers the report.
func (w *WMail) graphComplaintMIME(ctx context.Context, msg *models.EmailMessageData, fallback arf.Report) (arf.Report, error) {
	if w.EmailType != models.InboxProviderOutlook || w.GraphData == nil || w.GraphData.Client == nil || msg.GmailID == "" {
		return fallback, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	raw, err := w.GraphData.Client.DiagnosticRawMessage(ctx, msg.GmailID, complaintMIMEBytes)
	if err != nil {
		var mailErr *errx.MailError
		if errors.Is(err, msgraph.ErrRawMessageTooLarge) || errors.As(err, &mailErr) && mailErr.Failure != nil && mailErr.Failure.Disposition == errx.SendPermanent {
			return fallback, nil
		}
		return arf.Report{}, err
	}
	report := arf.ParseMIME(raw)
	clear(raw)
	return report, nil
}
