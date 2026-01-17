package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

const ResetPasswordTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
 <meta charset="utf-8"/>
 <title>{{.Subject}}</title>
 <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
 <style>
body,table,td{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;}
.wrapper{background-color:#f0f4ff;margin:0;padding:24px 0;}
.content{background-color:#fff;border-radius:8px;max-width:600px;margin:0 auto;padding:32px;border:1px solid #e1e8ff;}
.btn{background-color:#2563eb;border-radius:4px;color:#fff;display:inline-block;font-size:16px;font-weight:600;line-height:48px;text-align:center;text-decoration:none;width:220px;}
.btn:hover{background-color:#1e4fd1;}
.code{font-size:28px;font-weight:700;color:#1e3a8a;letter-spacing:4px;}
.footer{color:#6b7280;font-size:12px;padding-top:32px;text-align:center;}
img{border-radius:1em;}
 </style>
</head>
<body class="wrapper">
 <table role="presentation" cellspacing="0" cellpadding="0" border="0" width="100%">
 <tr>
 <td>
 <div class="content">
 <table role="presentation" width="100%">
 <tr>
 <td style="text-align:center;padding-bottom:24px;">
 <img src="https://warmbly.com/logo.jpg" alt="Warmbly" height="72"/>
 </td>
 </tr>
 </table>
 <p style="font-size:16px;color:#555555;line-height:24px;">
 We received a request to reset your password. Tap the button below to choose a new one.
 </p>
 <p style="text-align:center;margin:32px 0;">
 <a href="{{.ResetURL}}" class="btn">Reset Password</a>
 </p>
 <p style="font-size:14px;color:#888888;">
 If you didn't request this, you can safely ignore this email. The link expires in 4 hours.<br/><a href="{{.ResetURL}}" style="color:#22c55e;">{{.ResetURL}}</a>
 </p>
 <div class="footer">
 Warmbly.com | All rights reserved.<br/>
 </div>
 </div>
 </td>
 </tr>
 </table>
</body>
</html>
`

var ResetPasswordHTMLTMPL = template.Must(template.New("reset_password").Parse(LoginCodeTemplate))

func GenerateResetPasswordHTML(first_name, url string) (string, error) {
	var data struct {
		ResetURL string
	}
	data.ResetURL = url

	var body bytes.Buffer
	if err := ResetPasswordHTMLTMPL.Execute(&body, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}

	return body.String(), nil
}
