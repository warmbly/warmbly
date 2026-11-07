package templates

import (
	"bytes"
	"html/template"

	"github.com/warmbly/warmbly/internal/observability/errs"
)

// Trial-expiry notice rendered on the shared base shell so it matches
// the rest of the transactional mail instead of the old bare h2/ul
// fragment that shipped with no chrome at all.

const trialExpiredContent = `
<p style="margin:0 0 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;color:#94a3b8;letter-spacing:0.14em;text-transform:uppercase;font-weight:500;">
Subscription
</p>
<h2 style="margin:0 0 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-weight:600;font-size:20px;color:#0f172a;letter-spacing:-0.01em;">
Your free trial has ended
</h2>
<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
Thanks for trying {{.Brand}}. Your trial has now ended, so sending and warmup are paused. Your mailboxes keep their warmup settings, and warmup picks up where it stopped when you choose a plan.
</p>

<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 20px;width:100%;">
<tr><td style="padding:6px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">
&middot;&nbsp; Your data is safe and fully preserved
</td></tr>
<tr><td style="padding:6px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">
&middot;&nbsp; Campaigns are paused, not deleted
</td></tr>
<tr><td style="padding:6px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;line-height:20px;">
&middot;&nbsp; Connected mailboxes stay connected
</td></tr>
</table>

<p style="margin:0 0 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;color:#475569;line-height:20px;">
Choose a plan to switch everything back on:
</p>

<table cellpadding="0" cellspacing="0" border="0" align="center" role="presentation" style="margin:0 0 24px;">
<tr>
<td align="center" style="border-radius:6px;background:#0f172a;">
<a href="{{.BillingURL}}" target="_blank" style="display:inline-block;padding:10px 22px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:600;color:#ffffff;text-decoration:none;letter-spacing:0.01em;">Choose a plan</a>
</td>
</tr>
</table>

<div style="margin:0 0 16px;height:1px;background:#e2e8f0;"></div>

<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#64748b;line-height:18px;">
Questions about plans or billing? Reply to this email and our team will help.
</p>
`

var trialExpiredTmpl = template.Must(template.New("trial_expired_content").Parse(trialExpiredContent))

// GenerateTrialExpiredHTML renders the trial-ended notice through the
// shared base shell. The billing CTA points at the app's billing page.
func GenerateTrialExpiredHTML() (string, error) {
	brand := CompanyName()
	data := struct {
		BillingURL string
		Brand      string
	}{BillingURL: AppURL() + "/settings/billing", Brand: brand}
	var buf bytes.Buffer
	if err := trialExpiredTmpl.Execute(&buf, data); err != nil {
		errs.CaptureException(err)
		return "", err
	}
	return renderEmail("Your "+brand+" trial has ended", buf.String())
}
