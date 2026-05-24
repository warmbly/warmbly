package templates

import (
	"bytes"
	"html/template"
)

// ─── Centralized Business Details ────────────────────────────────
// Update these constants to change the branding and legal info
// across all email templates.
const (
	CompanyName    = "Warmbly"
	LegalEntity    = "Mindroot Ltd"
	CompanyNumber  = "00000000"
	PlaceOfReg     = "England and Wales"
	RegisteredAddr = "1 Example Street, London, W1A 1AA"
	WebsiteURL     = "https://warmbly.com"
	AppURL         = "https://app.warmbly.com"
	SupportEmail   = "support@warmbly.com"
	TermsURL       = "https://warmbly.com/terms"
	PrivacyURL     = "https://warmbly.com/privacy"
)

type baseData struct {
	Subject        string
	Content        template.HTML
	CompanyName    string
	LegalEntity    string
	CompanyNumber  string
	PlaceOfReg     string
	RegisteredAddr string
	WebsiteURL     string
	TermsURL       string
	PrivacyURL     string
}

var baseTmpl = template.Must(template.New("base").Parse(baseHTML))

func renderEmail(subject, content string) (string, error) {
	data := baseData{
		Subject:        subject,
		Content:        template.HTML(content),
		CompanyName:    CompanyName,
		LegalEntity:    LegalEntity,
		CompanyNumber:  CompanyNumber,
		PlaceOfReg:     PlaceOfReg,
		RegisteredAddr: RegisteredAddr,
		WebsiteURL:     WebsiteURL,
		TermsURL:       TermsURL,
		PrivacyURL:     PrivacyURL,
	}
	var buf bytes.Buffer
	if err := baseTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Dashboard-style transactional email shell.
//
// Replaces the previous radial-blue-gradient marketing-y design with
// the same chrome the user sees in the dashboard:
//   - clean cream background (#f5f6f8),
//   - white card with a hairline #e2e8f0 border,
//   - slate type, no fancy gradients,
//   - slate-900 logo monogram, no decorative haze.
const baseHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1.0"/>
<meta name="color-scheme" content="light"/>
<meta name="supported-color-schemes" content="light"/>
<title>{{.Subject}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f5f6f8;-webkit-text-size-adjust:100%;-ms-text-size-adjust:100%;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#0f172a;">

<table width="100%" cellpadding="0" cellspacing="0" border="0" role="presentation" style="background-color:#f5f6f8;">

<tr>
<td align="center" style="padding:40px 24px 20px;">
<table cellpadding="0" cellspacing="0" border="0" role="presentation">
<tr>
<td valign="middle" style="padding-right:10px;line-height:0;">
<svg width="22" height="22" viewBox="0 0 746 764" fill="none" xmlns="http://www.w3.org/2000/svg" style="display:block;">
<path d="M222.805 644.772L186.274 108.881L704.5 451.158L484.5 451.158L245.5 196.158L444 463.5L222.805 644.772Z" fill="#0f172a"/>
</svg>
</td>
<td valign="middle">
<span style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:700;font-size:15px;color:#0f172a;letter-spacing:-0.01em;">{{.CompanyName}}</span>
</td>
</tr>
</table>
</td>
</tr>

<tr>
<td align="center" style="padding:0 24px;">
<table cellpadding="0" cellspacing="0" border="0" width="520" align="center" role="presentation" style="max-width:520px;width:100%;background-color:#ffffff;border:1px solid #e2e8f0;border-radius:8px;">
<tr>
<td style="padding:32px 36px;">
{{.Content}}
</td>
</tr>
</table>
</td>
</tr>

<tr>
<td align="center" style="padding:32px 24px 48px;">
<table cellpadding="0" cellspacing="0" border="0" role="presentation" style="max-width:520px;width:100%;">
<tr>
<td align="center" style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;line-height:18px;padding-bottom:8px;">
<a href="{{.PrivacyURL}}" style="color:#64748b;text-decoration:none;">Privacy</a>
<span style="color:#cbd5e1;">&nbsp;·&nbsp;</span>
<a href="{{.TermsURL}}" style="color:#64748b;text-decoration:none;">Terms</a>
<span style="color:#cbd5e1;">&nbsp;·&nbsp;</span>
<a href="{{.WebsiteURL}}" style="color:#64748b;text-decoration:none;">warmbly.com</a>
</td>
</tr>
<tr>
<td align="center" style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:10px;line-height:16px;color:#94a3b8;">
&copy; {{.LegalEntity}} &middot; {{.CompanyNumber}} &middot; {{.PlaceOfReg}}
</td>
</tr>
<tr>
<td align="center" style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:10px;line-height:16px;color:#94a3b8;padding-top:4px;">
{{.RegisteredAddr}}
</td>
</tr>
</table>
</td>
</tr>

</table>

</body>
</html>`
