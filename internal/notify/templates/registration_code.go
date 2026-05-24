package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

const registrationCodeContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:500;">
Verify
</p>
<h2 style="margin:0 0 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:18px;color:#0f172a;letter-spacing:-0.01em;">
Welcome to Warmbly
</h2>
<p style="margin:0 0 24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
Enter this code to finish creating your account.
</p>

<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:0 0 16px;">
<tr>
<td style="background:#f8fafc;border:1px solid #e2e8f0;border-radius:6px;padding:14px 28px;text-align:center;">
<span style="font-family:'SF Mono','Fira Mono','Roboto Mono','Courier New',monospace;font-size:26px;font-weight:600;color:#0f172a;letter-spacing:6px;line-height:1;">{{.Code}}</span>
</td>
</tr>
</table>

<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;text-align:center;">
Expires in 15 minutes
</p>
`

var registrationCodeTmpl = template.Must(template.New("registration_code_content").Parse(registrationCodeContent))

func GenerateRegistrationCodeHTML(code string) (string, error) {
	data := struct{ Code string }{Code: code}
	var buf bytes.Buffer
	if err := registrationCodeTmpl.Execute(&buf, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	return renderEmail("Your Verification Code", buf.String())
}
