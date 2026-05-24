package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

// Welcome template — also moved onto the shared base shell so it
// matches the dashboard chrome instead of the old standalone HTML
// with its blue gradients and big blue button.

const welcomeContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:500;">
Welcome
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:20px;color:#0f172a;letter-spacing:-0.01em;">
{{if .FirstName}}Hi {{.FirstName}}, welcome to Warmbly{{else}}Welcome to Warmbly{{end}}
</h2>
<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
Your account is ready. From here you can connect mailboxes, warm them up, and start outbound campaigns — all in one place.
</p>

<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 20px;width:100%;">
<tr><td style="padding:6px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">
&middot;&nbsp; Connect Gmail, Outlook, or any SMTP/IMAP inbox
</td></tr>
<tr><td style="padding:6px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">
&middot;&nbsp; Automated warmup so your mail lands in primary
</td></tr>
<tr><td style="padding:6px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">
&middot;&nbsp; Sequences with scheduling, replies, opens, clicks
</td></tr>
<tr><td style="padding:6px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">
&middot;&nbsp; Unified inbox across every mailbox
</td></tr>
</table>

<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:0 0 24px;">
<tr>
<td align="center" style="border-radius:6px;background:#0f172a;">
<a href="https://app.warmbly.com/" target="_blank" style="display:inline-block;padding:10px 22px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:600;color:#ffffff;text-decoration:none;letter-spacing:0.01em;">Open the dashboard</a>
</td>
</tr>
</table>

<div style="margin:0 0 16px;height:1px;background:#e2e8f0;"></div>

<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#64748b;line-height:18px;">
Need help? Reply to this email or hit the support channel from your dashboard.
</p>
`

var welcomeTmpl = template.Must(template.New("welcome_content").Parse(welcomeContent))

// GenerateWelcomeHTML renders the welcome email through the shared
// base shell so styling stays consistent with the rest of the
// transactional mail.
func GenerateWelcomeHTML(firstName string) (string, error) {
	data := struct{ FirstName string }{FirstName: firstName}
	var buf bytes.Buffer
	if err := welcomeTmpl.Execute(&buf, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	return renderEmail("Welcome to Warmbly", buf.String())
}

// WelcomeTemplate / WelcomeHTMLTMPL retained as deprecated exports so
// any external caller importing them still compiles. New code should
// call GenerateWelcomeHTML instead.
const WelcomeTemplate = welcomeContent

var WelcomeHTMLTMPL = welcomeTmpl
