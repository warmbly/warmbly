package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

const loginCodeContent = `
<h2 style="margin:0 0 6px;font-family:'DM Serif Display',Georgia,'Times New Roman',serif;font-weight:400;font-size:26px;color:#0f172a;letter-spacing:-0.01em;">Hi there,</h2>
<p style="margin:0 0 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;color:#64748b;line-height:24px;">
Enter this code to verify your login.
</p>

<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:24px auto 28px;">
<tr>
<td style="background:linear-gradient(135deg,#f0f9ff 0%,#e0f2fe 100%);border:1.5px solid #bae6fd;border-radius:14px;padding:16px 36px;text-align:center;">
<span style="font-family:'SF Mono','Fira Mono','Roboto Mono','Courier New',monospace;font-size:30px;font-weight:700;color:#0c4a6e;letter-spacing:8px;line-height:1;">{{.Code}}</span>
</td>
</tr>
</table>

<p style="margin:0 0 20px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#94a3b8;text-align:center;">This code expires in 15 minutes.</p>

<div style="margin:0 0 16px;height:1px;background:linear-gradient(to right,transparent,#e2e8f0,transparent);"></div>

<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#94a3b8;line-height:20px;">
If you didn't request this code, you can safely ignore this email.
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
