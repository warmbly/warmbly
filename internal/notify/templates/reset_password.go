package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

const resetPasswordContent = `
<h2 style="margin:0 0 6px;font-family:'DM Serif Display',Georgia,'Times New Roman',serif;font-weight:400;font-size:26px;color:#0f172a;letter-spacing:-0.01em;">Reset your password</h2>
<p style="margin:0 0 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;color:#64748b;line-height:24px;">
We received a request to reset your password. Click the button below to choose a new one.
</p>

<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:24px auto 28px;">
<tr>
<td align="center" style="border-radius:10px;background:linear-gradient(to bottom,#0ea5e9,#0284c7);box-shadow:0 4px 14px rgba(14,165,233,0.35);">
<a href="{{.ResetURL}}" target="_blank" style="display:inline-block;padding:14px 44px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;font-weight:600;color:#ffffff;text-decoration:none;letter-spacing:0.01em;">Reset Password</a>
</td>
</tr>
</table>

<div style="margin:0 0 16px;height:1px;background:linear-gradient(to right,transparent,#e2e8f0,transparent);"></div>

<p style="margin:0 0 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#94a3b8;line-height:20px;">
If you didn't request this, you can safely ignore this email. The link expires in 4 hours.
</p>
<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#94a3b8;word-break:break-all;">
<a href="{{.ResetURL}}" style="color:#0ea5e9;text-decoration:none;">{{.ResetURL}}</a>
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
