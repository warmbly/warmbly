package templates

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"

	"github.com/warmbly/warmbly/internal/observability/errs"
)

// ClientReportEmail is a scheduled client report summary. Every name in it must already be display-safe.
type ClientReportEmail struct {
	BrandName   string
	LogoURL     string
	AccentColor string
	ReportName  string
	Period      string
	Metrics     []ClientReportMetric
	Campaigns   []ClientReportEmailCampaign
	ReportURL   string
	// PoweredByName and PoweredByURL are the deployment's attribution, empty when the brand removed it.
	PoweredByName string
	PoweredByURL  string
}

// ClientReportMetric is one KPI tile.
type ClientReportMetric struct {
	Label string
	Value string
	Note  string
}

// ClientReportEmailCampaign is one row of the email's campaign summary.
type ClientReportEmailCampaign struct {
	Name    string
	Sent    string
	Replies string
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

const clientReportFont = `-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif`

const clientReportHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1.0"/>
<meta name="color-scheme" content="light"/>
<title>{{.ReportName}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f5f6f8;font-family:{{.Font}};color:#0f172a;">
<table width="100%" cellpadding="0" cellspacing="0" border="0" role="presentation" style="background-color:#f5f6f8;">
<tr><td align="center" style="padding:36px 24px 18px;">
{{if .LogoURL}}<img src="{{.LogoURL}}" alt="{{.BrandName}}" height="28" style="display:block;height:28px;max-width:180px;border:0;"/>{{else}}<span style="font-family:{{.Font}};font-weight:700;font-size:15px;color:#0f172a;">{{.BrandName}}</span>{{end}}
</td></tr>
<tr><td align="center" style="padding:0 24px;">
<table cellpadding="0" cellspacing="0" border="0" width="560" role="presentation" style="max-width:560px;width:100%;background-color:#ffffff;border:1px solid #e2e8f0;border-radius:8px;">
<tr><td style="padding:28px 32px 8px;">
<p style="margin:0 0 4px;font-family:{{.Font}};font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:500;">{{.Period}}</p>
<h2 style="margin:0 0 20px;font-family:{{.Font}};font-weight:600;font-size:20px;color:#0f172a;">{{.ReportName}}</h2>
</td></tr>
<tr><td style="padding:0 26px 8px;">
<table width="100%" cellpadding="0" cellspacing="0" border="0" role="presentation"><tr>
{{range .Metrics}}<td valign="top" style="padding:0 6px 12px;">
<div style="border:1px solid #e2e8f0;border-radius:6px;padding:10px 12px;">
<div style="font-family:{{$.Font}};font-size:10px;color:#64748b;letter-spacing:0.08em;text-transform:uppercase;">{{.Label}}</div>
<div style="font-family:{{$.Font}};font-size:20px;font-weight:600;color:#0f172a;padding-top:4px;">{{.Value}}</div>
{{if .Note}}<div style="font-family:{{$.Font}};font-size:11px;color:#64748b;padding-top:2px;">{{.Note}}</div>{{end}}
</div>
</td>{{end}}
</tr></table>
</td></tr>
{{if .Campaigns}}<tr><td style="padding:4px 32px 8px;">
<table width="100%" cellpadding="0" cellspacing="0" border="0" role="presentation" style="font-family:{{.Font}};font-size:12.5px;color:#334155;">
<tr><td style="padding:6px 0;border-bottom:1px solid #e2e8f0;color:#64748b;font-size:11px;">Campaign</td><td align="right" style="padding:6px 0;border-bottom:1px solid #e2e8f0;color:#64748b;font-size:11px;">Sent</td><td align="right" style="padding:6px 0;border-bottom:1px solid #e2e8f0;color:#64748b;font-size:11px;">Replies</td></tr>
{{range .Campaigns}}<tr><td style="padding:7px 8px 7px 0;border-bottom:1px solid #f1f5f9;">{{.Name}}</td><td align="right" style="padding:7px 0;border-bottom:1px solid #f1f5f9;">{{.Sent}}</td><td align="right" style="padding:7px 0;border-bottom:1px solid #f1f5f9;">{{.Replies}}</td></tr>{{end}}
</table>
</td></tr>{{end}}
<tr><td align="center" style="padding:20px 32px 30px;">
<table cellpadding="0" cellspacing="0" border="0" role="presentation"><tr>
<td align="center" style="border-radius:6px;background:{{.Accent}};">
<a href="{{.ReportURL}}" target="_blank" style="display:inline-block;padding:10px 22px;font-family:{{.Font}};font-size:13px;font-weight:600;color:#ffffff;text-decoration:none;">View full report</a>
</td></tr></table>
</td></tr>
</table>
</td></tr>
<tr><td align="center" style="padding:24px 24px 44px;font-family:{{.Font}};font-size:11px;color:#94a3b8;">
{{.BrandName}}{{if .PoweredByName}}<span style="color:#cbd5e1;">&nbsp;·&nbsp;</span>{{if .PoweredByURL}}<a href="{{.PoweredByURL}}" style="color:#94a3b8;text-decoration:none;">Powered by {{.PoweredByName}}</a>{{else}}Powered by {{.PoweredByName}}{{end}}{{end}}
</td></tr>
</table>
</body>
</html>`

var clientReportTmpl = template.Must(template.New("client_report").Parse(clientReportHTML))

// GenerateClientReportHTML renders a scheduled client report email.
func GenerateClientReportHTML(in ClientReportEmail) (string, error) {
	accent := "#0f172a"
	if hexColor.MatchString(in.AccentColor) {
		accent = in.AccentColor
	}
	if !strings.HasPrefix(in.LogoURL, "https://") {
		in.LogoURL = ""
	}
	data := struct {
		ClientReportEmail
		Accent template.CSS
		Font   template.CSS
	}{ClientReportEmail: in, Accent: template.CSS(accent), Font: template.CSS(clientReportFont)}
	var buf bytes.Buffer
	if err := clientReportTmpl.Execute(&buf, data); err != nil {
		errs.CaptureException(err)
		return "", err
	}
	return buf.String(), nil
}
