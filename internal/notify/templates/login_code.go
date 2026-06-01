package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

// Dashboard-style content. Small uppercase eyebrow, slate-900 plain
// h2 (no serif), neutral grey body. Code is rendered in a tight
// monospace pill with a hairline border — matches how short codes
// look in the dashboard chrome.
const loginCodeContent = `
<p style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:600;">
Sign in
</p>
<h2 style="margin:0 0 10px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:19px;color:#0f172a;letter-spacing:-0.01em;line-height:1.3;">
Your login code
</h2>
<p style="margin:0 0 24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
Enter this code on the sign-in screen to finish logging in.
</p>

<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:0 0 16px;">
<tr>
<td style="background:#f8fafc;border:1px solid #e2e8f0;border-radius:6px;padding:14px 28px;text-align:center;">
<span style="font-family:'SF Mono','Fira Mono','Roboto Mono','Courier New',monospace;font-size:26px;font-weight:600;color:#0f172a;letter-spacing:6px;line-height:1;">{{.Code}}</span>
</td>
</tr>
</table>

<p style="margin:0 0 20px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;text-align:center;">
Expires in 15 minutes
</p>

<div style="margin:0 0 16px;height:1px;background:#e2e8f0;"></div>

<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#94a3b8;line-height:18px;">
If you didn't request this, you can safely ignore the email.
</p>
`

var loginCodeTmpl = template.Must(template.New("login_code_content").Parse(loginCodeContent))

func GenerateLoginCodeHTML(code string) (string, error) {
	data := struct{ Code string }{Code: code}
	var buf bytes.Buffer
	if err := loginCodeTmpl.Execute(&buf, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	return renderEmail("Your Login Code", buf.String())
}
