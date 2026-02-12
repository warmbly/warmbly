package templates

import (
	"bytes"
	"html/template"
)

// ─── Centralized Business Details ────────────────────────────────
// Update these constants to change the branding and legal info
// across all email templates.
//
// Required by the Companies Act 2006 (s.82) for a company
// registered in England and Wales:
//   - Registered name
//   - Company number (Companies House)
//   - Place of registration
//   - Registered office address
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

const baseHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1.0"/>
<meta name="color-scheme" content="light"/>
<meta name="supported-color-schemes" content="light"/>
<title>{{.Subject}}</title>
<!--[if !mso]><!-->
<style>
@import url('https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:wght@800&family=DM+Serif+Display&display=swap');
</style>
<!--<![endif]-->
</head>
<body style="margin:0;padding:0;background-color:#0284c7;-webkit-text-size-adjust:100%;-ms-text-size-adjust:100%;">

<!--[if !mso]><!-->
<div style="position:relative;overflow:hidden;background:radial-gradient(ellipse 140% 140% at 72% 25%,#38bdf8 0%,#0ea5e9 18%,#0284c7 36%,#075985 58%,#0c4a6e 82%);min-height:100%;">

<!-- Sun glow -->
<div style="position:absolute;top:-80px;right:-40px;width:550px;height:550px;background:radial-gradient(circle,rgba(253,224,71,0.25) 0%,rgba(253,186,116,0.12) 25%,rgba(56,189,248,0.04) 50%,transparent 65%);border-radius:50%;pointer-events:none;" aria-hidden="true"></div>

<!-- Horizon haze -->
<div style="position:absolute;bottom:0;left:0;right:0;height:45%;background:linear-gradient(to top,rgba(125,211,252,0.18) 0%,rgba(56,189,248,0.08) 30%,transparent 100%);pointer-events:none;" aria-hidden="true"></div>
<!--<![endif]-->

<table width="100%" cellpadding="0" cellspacing="0" border="0" role="presentation" style="position:relative;z-index:1;">

<!-- Logo + Brand -->
<tr>
<td align="center" style="padding:48px 24px 24px;">
<table cellpadding="0" cellspacing="0" border="0" role="presentation">
<tr>
<td valign="middle" style="padding-right:10px;line-height:0;">
<!--[if !mso]><!-->
<svg width="38" height="39" viewBox="0 0 746 764" fill="none" xmlns="http://www.w3.org/2000/svg" style="display:block;">
<path d="M222.805 644.772L186.274 108.881L704.5 451.158L484.5 451.158L245.5 196.158L444 463.5L222.805 644.772Z" fill="white"/>
</svg>
<!--<![endif]-->
</td>
<td valign="middle">
<span style="font-family:'Bricolage Grotesque',-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;font-weight:800;font-size:22px;color:#ffffff;letter-spacing:-0.02em;">{{.CompanyName}}</span>
</td>
</tr>
</table>
</td>
</tr>

<!-- Card -->
<tr>
<td align="center" style="padding:0 24px;">
<!--[if mso]>
<table cellpadding="0" cellspacing="0" border="0" width="520" align="center"><tr><td style="background-color:#ffffff;padding:40px 44px;border:1px solid #bae6fd;">
<![endif]-->
<!--[if !mso]><!-->
<div style="max-width:520px;width:100%;background:rgba(255,255,255,0.96);border-radius:20px;border:1px solid rgba(186,230,253,0.4);box-shadow:0 8px 40px -12px rgba(56,189,248,0.18),0 4px 25px -5px rgba(0,0,0,0.08);padding:40px 44px;text-align:center;">
<!--<![endif]-->

{{.Content}}

<!--[if mso]>
</td></tr></table>
<![endif]-->
<!--[if !mso]><!-->
</div>
<!--<![endif]-->
</td>
</tr>

<!-- Footer -->
<tr>
<td align="center" style="padding:44px 24px 48px;">
<table cellpadding="0" cellspacing="0" border="0" role="presentation" style="max-width:320px;width:100%;">
<tr>
<td align="center" style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;line-height:11px;padding-bottom:10px;"><a href="{{.PrivacyURL}}" style="color:rgba(255,255,255,0.40);text-decoration:none;">Privacy Policy</a><span style="color:rgba(255,255,255,0.20);"> &middot; </span><a href="{{.TermsURL}}" style="color:rgba(255,255,255,0.40);text-decoration:none;">Terms of Use</a></td>
</tr>
<tr>
<td align="center" style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:10px;line-height:16px;color:rgba(255,255,255,0.18);padding-bottom:10px;">&copy; {{.LegalEntity}} &middot; {{.CompanyNumber}} &middot; {{.PlaceOfReg}} &middot; {{.RegisteredAddr}}</td>
</tr>
<tr>
<td align="center" style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;line-height:11px;"><a href="{{.WebsiteURL}}" style="color:rgba(255,255,255,0.40);text-decoration:none;">warmbly.com</a></td>
</tr>
</table>
</td>
</tr>

</table>

<!--[if !mso]><!-->
</div>
<!--<![endif]-->

</body>
</html>`
