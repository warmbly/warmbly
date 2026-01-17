package templates

import (
	"bytes"
	"html/template"

	"github.com/getsentry/sentry-go"
)

const LoginCodeTemplate = `
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
 {{if eq .Type "welcome"}}
 <h2 style="margin:0 0 12px;font-size:24px;">Welcome to Warmbly! 🎉</h2>
 {{else}}
 <h2 style="margin:0 0 12px;font-size:24px;color:#333333;">Hi,</h2>
 {{end}}
 {{if eq .Type "verify"}}
 <p style="font-size:16px;color:#555555;line-height:24px;">
 Please enter the code below in the app to finish verifying your email.
 </p>
 <p style="text-align:center;margin:32px 0;">
 <span class="code">{{.Code}}</span>
 </p>
 <p style="font-size:14px;color:#888888;">This code expires in 15 minutes.</p>
 {{else if eq .Type "reset"}}
 <p style="font-size:16px;color:#555555;line-height:24px;">
 We received a request to reset your password. Tap the button below to choose a new one.
 </p>
 <p style="text-align:center;margin:32px 0;">
 <a href="{{.ResetURL}}" class="btn">Reset Password</a>
 </p>
 <p style="font-size:14px;color:#888888;">
 If you didn't request this, you can safely ignore this email. The link expires in 4 hours.<br/><a href="{{.ResetURL}}" style="color:#22c55e;">{{.ResetURL}}</a>
 </p>
 {{else if eq .Type "welcome"}}
 <p style="font-size:16px;color:#374151;line-height:24px;">
We’re thrilled to have you on board! <strong>Warmbly</strong> is your all‑in‑one cold email delivery platform – built for developers, sales teams, and anyone who needs reliable email outreach.
</p>

<p style="font-size:16px;color:#374151;line-height:24px;">
With Warmbly you can:
</p>

<ul style="font-size:16px;color:#374151;line-height:24px;margin:16px 0;padding-left:20px;">
  <li>📩 <strong>Send cold emails at scale</strong> with a clean, intuitive dashboard</li>
  <li>🤖 <strong>Automate warmup & deliverability</strong> so your emails actually land</li>
  <li>📊 <strong>Track performance</strong> — opens, clicks, replies, all in one place</li>
  <li>🛠️ <strong>Integrate with your apps</strong> via API & SDK if you need full control</li>
</ul>

<p style="font-size:16px;color:#374151;line-height:24px;">
Whether you’re running outreach campaigns or building on top of our developer tools, Warmbly takes care of the hard stuff — from warmup to deliverability — so you can focus on results.
</p>

<p style="text-align:center;margin:32px 0;">
<a href="https://app.warmbly.com/" class="btn">Launch Your First Campaign</a>
</p>

<p style="font-size:14px;color:#6b7280;">
Need help getting started? Check out our <a href="https://warmbly.com/blog/getting-started" style="color:#2563eb;text-decoration:none;">getting started guide</a> or contact us on Discord. (<a href="https://dc.warmbly.com/" style="color:#2563eb;text-decoration:none;">link</a>)
</p>
 {{end}}
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

var LoginCodeHTMLTMPL = template.Must(template.New("login_code").Parse(LoginCodeTemplate))

func GenerateLoginCodeHTML(first_name, code string) (string, error) {
	var data struct {
		Code string
	}
	data.Code = code

	var body bytes.Buffer
	if err := ResetPasswordHTMLTMPL.Execute(&body, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}

	return body.String(), nil
}
