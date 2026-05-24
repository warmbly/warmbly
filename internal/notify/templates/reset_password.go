package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

const resetPasswordContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:500;">
Account
</p>
<h2 style="margin:0 0 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:18px;color:#0f172a;letter-spacing:-0.01em;">
Reset your password
</h2>
<p style="margin:0 0 24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
We received a request to reset your password. Click the button to choose a new one. If you didn't request this, you can safely ignore this email.
</p>

<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:0 0 24px;">
<tr>
<td align="center" style="border-radius:6px;background:#0f172a;">
<a href="{{.ResetURL}}" target="_blank" style="display:inline-block;padding:10px 22px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:600;color:#ffffff;text-decoration:none;letter-spacing:0.01em;">Reset password</a>
</td>
</tr>
</table>

<div style="margin:0 0 16px;height:1px;background:#e2e8f0;"></div>

<p style="margin:0 0 6px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.08em;text-transform:uppercase;font-weight:500;">
Link expires in 4 hours
</p>
<p style="margin:0;font-family:'SF Mono','Fira Mono','Roboto Mono','Courier New',monospace;font-size:11px;color:#64748b;word-break:break-all;line-height:18px;">
<a href="{{.ResetURL}}" style="color:#0f172a;text-decoration:none;">{{.ResetURL}}</a>
</p>
`

var resetPasswordTmpl = template.Must(template.New("reset_password_content").Parse(resetPasswordContent))

func GenerateResetPasswordHTML(firstName, url string) (string, error) {
	data := struct{ ResetURL string }{ResetURL: url}
	var buf bytes.Buffer
	if err := resetPasswordTmpl.Execute(&buf, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	return renderEmail("Reset Your Password", buf.String())
}
