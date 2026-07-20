package templates

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/getsentry/sentry-go"
)

// Notification digest: several pending notifications bundled into one email
// by the flush loop, so a busy stretch produces a single message instead of
// a stream. Items render newest-last with their own links; titles and bodies
// are plain text, auto-escaped by html/template.

// DigestItem is one bundled notification.
type DigestItem struct {
	Title string
	Body  string
	URL   string
}

const digestContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:500;">
Digest
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:19px;color:#0f172a;letter-spacing:-0.01em;line-height:1.3;">
While you were away
</h2>
<p style="margin:0 0 20px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
{{.Count}} updates in your Warmbly workspace, bundled into one email.
</p>
{{range .Items}}
<div style="margin:0 0 14px;padding:12px 14px;border:1px solid #e2e8f0;border-radius:8px;">
<p style="margin:0 0 2px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:600;color:#0f172a;line-height:19px;">
{{if .URL}}<a href="{{.URL}}" target="_blank" style="color:#0f172a;text-decoration:none;">{{.Title}}</a>{{else}}{{.Title}}{{end}}
</p>
{{if .Body}}
<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12.5px;color:#475569;line-height:19px;">
{{.Body}}
</p>
{{end}}
</div>
{{end}}
<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:8px 0 24px;">
<tr>
<td align="center" style="border-radius:6px;background:#0f172a;">
<a href="{{.AppURL}}" target="_blank" style="display:inline-block;padding:10px 22px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:600;color:#ffffff;text-decoration:none;letter-spacing:0.01em;">Open Warmbly</a>
</td>
</tr>
</table>
<div style="margin:0 0 16px;height:1px;background:#e2e8f0;"></div>

<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#94a3b8;line-height:18px;">
Updates you read in the app are never emailed, and the rest bundle on the cadence you chose. Manage both in your notification settings.
</p>
`

var digestTmpl = template.Must(template.New("digest_content").Parse(digestContent))

// GenerateDigestHTML renders the bundled-notifications email.
func GenerateDigestHTML(count int, items []DigestItem) (string, error) {
	data := struct {
		Count  int
		Items  []DigestItem
		AppURL string
	}{Count: count, Items: items, AppURL: AppURL}
	var buf bytes.Buffer
	if err := digestTmpl.Execute(&buf, data); err != nil {
		sentry.CaptureException(err)
		return "", err
	}
	return renderEmail(fmt.Sprintf("%d updates in your Warmbly workspace", count), buf.String())
}
