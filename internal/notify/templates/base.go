package templates

import (
	"bytes"
	"html/template"
	"os"
	"strings"

	"github.com/getsentry/sentry-go"
)

// ─── Centralized Business Details ────────────────────────────────
// Branding and legal info for every email template. These are variables, not
// constants, because a self-hosted install must not send mail attributed to
// Mindroot Ltd with links to someone else's dashboard: AppURL derives from
// APP_URL and the rest are overridable with EMAIL_BRAND_*.
var (
	CompanyName    = brandEnv("EMAIL_BRAND_NAME", "Warmbly")
	LegalEntity    = brandEnv("EMAIL_BRAND_LEGAL_ENTITY", "Mindroot Ltd")
	CompanyNumber  = brandEnv("EMAIL_BRAND_COMPANY_NUMBER", "00000000")
	PlaceOfReg     = brandEnv("EMAIL_BRAND_PLACE_OF_REG", "England and Wales")
	RegisteredAddr = brandEnv("EMAIL_BRAND_ADDRESS", "1 Example Street, London, W1A 1AA")
	WebsiteURL     = brandEnv("EMAIL_BRAND_WEBSITE_URL", "https://warmbly.com")
	AppURL         = appURL()
	SupportEmail   = brandEnv("EMAIL_BRAND_SUPPORT_EMAIL", "team@warmbly.com")
	TermsURL       = brandEnv("EMAIL_BRAND_TERMS_URL", "https://warmbly.com/terms")
	PrivacyURL     = brandEnv("EMAIL_BRAND_PRIVACY_URL", "https://warmbly.com/privacy")
)

func brandEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// appURL is the dashboard base every emailed link is built from. Reading it
// here rather than hardcoding it is what makes password reset and team invites
// work on a self-hosted install.
func appURL() string {
	for _, key := range []string{"APP_URL", "FRONTEND_BASE_URL"} {
		if v := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); v != "" {
			return v
		}
	}
	return "https://app.warmbly.com"
}

// WebsiteLabel is the display text for the footer website link, derived from
// WebsiteURL so a rebranded install does not render "warmbly.com" pointing
// somewhere else.
func WebsiteLabel() string {
	label := strings.TrimPrefix(strings.TrimPrefix(WebsiteURL, "https://"), "http://")
	return strings.TrimSuffix(label, "/")
}

type baseData struct {
	Subject        string
	Content        template.HTML
	CompanyName    string
	LegalEntity    string
	CompanyNumber  string
	PlaceOfReg     string
	RegisteredAddr string
	WebsiteURL     string
	WebsiteLabel   string
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
		WebsiteLabel:   WebsiteLabel(),
		TermsURL:       TermsURL,
		PrivacyURL:     PrivacyURL,
	}
	var buf bytes.Buffer
	if err := baseTmpl.Execute(&buf, data); err != nil {
		sentry.CaptureException(err)
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
<a href="{{.WebsiteURL}}" style="color:#64748b;text-decoration:none;">{{.WebsiteLabel}}</a>
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
