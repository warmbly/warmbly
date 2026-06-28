package templates

import (
	"bytes"
	"fmt"
	"html/template"
	"time"

	"github.com/getsentry/sentry-go"
)

// Danger-zone (scheduled deletion) emails. These move off the old
// standalone wrapper onto the shared base shell so they match the rest
// of the transactional mail, while keeping semantic accent colours:
// red for a scheduled deletion, amber for an approaching reminder,
// green for a cancellation. Resource/user names interpolate through
// html/template and are auto-escaped; dates are pre-formatted strings.

// formatDeletionTime renders an absolute UTC timestamp. People skim
// these on phones, so every date is unambiguous and timezone-explicit.
func formatDeletionTime(t time.Time) string {
	return t.UTC().Format("Monday, 02 January 2006 at 15:04 UTC")
}

const deletionFooterNote = `
<div style="margin:0 0 16px;height:1px;background:#e2e8f0;"></div>
<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#94a3b8;line-height:18px;">
You're receiving this because a destructive action was scheduled on your account. If you didn't request this, cancel it right away and reset your password.
</p>
`

// ─── Organization scheduled ─────────────────────────────────────────

const orgDeletionScheduledContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#b91c1c;letter-spacing:0.14em;text-transform:uppercase;font-weight:600;">
Scheduled deletion
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:20px;color:#0f172a;letter-spacing:-0.01em;">
Organization scheduled for deletion
</h2>
<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
Your organization <strong style="color:#0f172a;">{{.Name}}</strong> has been scheduled for permanent deletion.
</p>
` + deletionDetailBlock + `
<p style="margin:0 0 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.08em;text-transform:uppercase;font-weight:500;">
What happens now
</p>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 20px;width:100%;">
<tr><td style="padding:4px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">&middot;&nbsp; Campaigns keep running until the deletion date</td></tr>
<tr><td style="padding:4px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">&middot;&nbsp; All members keep access during the grace period</td></tr>
<tr><td style="padding:4px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">&middot;&nbsp; On the deletion date, the organization and all its data are permanently removed</td></tr>
</table>
` + deletionCancelButton + deletionFooterNote

// ─── User scheduled ─────────────────────────────────────────────────

const userDeletionScheduledContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#b91c1c;letter-spacing:0.14em;text-transform:uppercase;font-weight:600;">
Scheduled deletion
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:20px;color:#0f172a;letter-spacing:-0.01em;">
Account scheduled for deletion
</h2>
<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
Hi {{.Name}}, your Warmbly account has been scheduled for permanent deletion.
</p>
` + deletionDetailBlock + `
<p style="margin:0 0 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.08em;text-transform:uppercase;font-weight:500;">
What happens now
</p>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 20px;width:100%;">
<tr><td style="padding:4px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">&middot;&nbsp; You can keep using your account during the grace period</td></tr>
<tr><td style="padding:4px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">&middot;&nbsp; Cancelling any time before the deletion date keeps it intact</td></tr>
<tr><td style="padding:4px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">&middot;&nbsp; On the deletion date, your account and all owned data are permanently removed</td></tr>
</table>
` + deletionCancelButton + deletionFooterNote

// Shared red callout: the one fact that matters, the date, plus grace.
// Background/border live on the <td>, not the <table>: Outlook's Word
// engine ignores those properties on <table> (the OTP code pill uses
// the same td-level pattern).
const deletionDetailBlock = `
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="margin:0 0 20px;">
<tr><td style="background:#fef2f2;border:1px solid #fecaca;border-radius:6px;padding:14px 16px;">
<p style="margin:0 0 6px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#b91c1c;letter-spacing:0.08em;text-transform:uppercase;font-weight:600;">Will be deleted on</p>
<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;color:#0f172a;font-weight:600;line-height:20px;">{{.DeleteOn}}</p>
<p style="margin:10px 0 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#b91c1c;line-height:18px;">Grace period: {{.GraceDays}} days. You can cancel any time before that date.</p>
</td></tr>
</table>
`

// Shared neutral cancel CTA (slate, on-brand) used by scheduled mails.
const deletionCancelButton = `
<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:0 0 24px;">
<tr>
<td align="center" style="border-radius:6px;background:#0f172a;">
<a href="{{.CancelURL}}" target="_blank" style="display:inline-block;padding:10px 22px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:600;color:#ffffff;text-decoration:none;letter-spacing:0.01em;">Cancel deletion</a>
</td>
</tr>
</table>
`

// ─── Cancelled (org + user) ─────────────────────────────────────────

const deletionCancelledContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#047857;letter-spacing:0.14em;text-transform:uppercase;font-weight:600;">
Deletion cancelled
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:20px;color:#0f172a;letter-spacing:-0.01em;">
{{.Heading}}
</h2>
<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
{{if .IsUser}}Hi {{.Name}}, the scheduled deletion of your account has been cancelled. Your account is active and back to normal.{{else}}The scheduled deletion for <strong style="color:#0f172a;">{{.Name}}</strong> has been cancelled. Your organization is safe and operating normally.{{end}}
</p>
<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#94a3b8;line-height:18px;">
Originally scheduled for: {{.OriginalDate}}
</p>
`

// ─── Reminder ───────────────────────────────────────────────────────

const deletionReminderContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#b45309;letter-spacing:0.14em;text-transform:uppercase;font-weight:600;">
Reminder
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:20px;color:#0f172a;letter-spacing:-0.01em;">
Deletion in {{.Window}}
</h2>
<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
<strong style="color:#0f172a;">{{.Name}}</strong> is scheduled to be permanently deleted on <strong style="color:#0f172a;">{{.DeleteOn}}</strong>.
</p>
<p style="margin:0 0 20px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
If you didn't mean to do this, cancel now while you still can. After the deletion runs, recovery is not possible.
</p>
` + deletionCancelButton + deletionFooterNote

// ─── Completed ──────────────────────────────────────────────────────

const deletionCompletedContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:500;">
Deletion completed
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:20px;color:#0f172a;letter-spacing:-0.01em;">
Deletion completed
</h2>
<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
The scheduled deletion has been completed. All associated data has been permanently removed.
</p>
<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#94a3b8;line-height:18px;">
Scheduled at: {{.ScheduledAt}}<br>Executed at: {{.ExecutedAt}}
</p>
`

var (
	orgDeletionScheduledTmpl  = template.Must(template.New("org_deletion_scheduled").Parse(orgDeletionScheduledContent))
	userDeletionScheduledTmpl = template.Must(template.New("user_deletion_scheduled").Parse(userDeletionScheduledContent))
	deletionCancelledTmpl     = template.Must(template.New("deletion_cancelled").Parse(deletionCancelledContent))
	deletionReminderTmpl      = template.Must(template.New("deletion_reminder").Parse(deletionReminderContent))
	deletionCompletedTmpl     = template.Must(template.New("deletion_completed").Parse(deletionCompletedContent))
)

func renderDeletion(tmpl *template.Template, subject string, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	return renderEmail(subject, buf.String())
}

// GenerateOrgDeletionScheduledHTML renders the org "scheduled for
// deletion" notice.
func GenerateOrgDeletionScheduledHTML(orgName string, executeAfter time.Time, graceDays int, cancelURL string) (string, error) {
	return renderDeletion(orgDeletionScheduledTmpl, "Organization scheduled for deletion", struct {
		Name      string
		DeleteOn  string
		GraceDays int
		CancelURL string
	}{orgName, formatDeletionTime(executeAfter), graceDays, cancelURL})
}

// GenerateUserDeletionScheduledHTML renders the account "scheduled for
// deletion" notice. firstName falls back to the email upstream.
func GenerateUserDeletionScheduledHTML(firstName string, executeAfter time.Time, graceDays int, cancelURL string) (string, error) {
	return renderDeletion(userDeletionScheduledTmpl, "Your Warmbly account is scheduled for deletion", struct {
		Name      string
		DeleteOn  string
		GraceDays int
		CancelURL string
	}{firstName, formatDeletionTime(executeAfter), graceDays, cancelURL})
}

// GenerateOrgDeletionCancelledHTML renders the org deletion-cancelled
// confirmation. The org name is interpolated (and auto-escaped) by the
// template rather than pre-formatted into the message.
func GenerateOrgDeletionCancelledHTML(orgName string, originalDate time.Time) (string, error) {
	return renderDeletion(deletionCancelledTmpl, "Deletion cancelled", struct {
		Heading      string
		IsUser       bool
		Name         string
		OriginalDate string
	}{"Deletion cancelled", false, orgName, formatDeletionTime(originalDate)})
}

// GenerateUserDeletionCancelledHTML renders the account deletion-
// cancelled confirmation.
func GenerateUserDeletionCancelledHTML(firstName string, originalDate time.Time) (string, error) {
	return renderDeletion(deletionCancelledTmpl, "Account deletion cancelled", struct {
		Heading      string
		IsUser       bool
		Name         string
		OriginalDate string
	}{"Account deletion cancelled", true, firstName, formatDeletionTime(originalDate)})
}

// GenerateDeletionReminderHTML renders an approaching-deletion reminder.
// The human-friendly window is derived from how long is left.
func GenerateDeletionReminderHTML(resourceName string, executeAfter time.Time, cancelURL string) (string, error) {
	hours := int(time.Until(executeAfter).Hours())
	var window string
	switch {
	case hours <= 24:
		window = "less than 24 hours"
	case hours <= 24*8:
		window = fmt.Sprintf("about %d days", hours/24)
	default:
		window = fmt.Sprintf("%d days", hours/24)
	}
	return renderDeletion(deletionReminderTmpl, "Deletion reminder", struct {
		Window    string
		Name      string
		DeleteOn  string
		CancelURL string
	}{window, resourceName, formatDeletionTime(executeAfter), cancelURL})
}

// GenerateDeletionCompletedHTML renders the post-deletion confirmation.
func GenerateDeletionCompletedHTML(scheduledAt, executedAt time.Time) (string, error) {
	return renderDeletion(deletionCompletedTmpl, "Deletion completed", struct {
		ScheduledAt string
		ExecutedAt  string
	}{formatDeletionTime(scheduledAt), formatDeletionTime(executedAt)})
}
