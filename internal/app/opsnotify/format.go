package opsnotify

import (
	"fmt"
	"html"
	"strings"
)

// Chat transports colour by severity. Discord takes a decimal integer, Slack
// takes a hex string on the attachment.
func severityColorInt(s Severity) int {
	switch s {
	case SeverityUrgent:
		return 0xDC2626 // red-600
	case SeverityWarning:
		return 0xD97706 // amber-600
	default:
		return 0x0284C7 // sky-600
	}
}

func severityColorHex(s Severity) string {
	return fmt.Sprintf("#%06X", severityColorInt(s))
}

// discordPayload builds a single embed. Discord caps a field value at 1024
// characters and an embed at 25 fields; both are enforced here so a long
// value cannot make the whole POST fail.
func discordPayload(e Event) map[string]any {
	fields := make([]map[string]any, 0, len(e.Fields))
	for i, f := range e.Fields {
		if i >= 25 {
			break
		}
		fields = append(fields, map[string]any{
			"name":   truncate(f.Label, 256),
			"value":  truncate(emptyDash(f.Value), 1024),
			"inline": len(f.Value) <= 40,
		})
	}
	embed := map[string]any{
		"title":       truncate(e.Title, 256),
		"description": truncate(e.Summary, 4096),
		"color":       severityColorInt(e.Severity),
		"footer":      map[string]any{"text": "Warmbly"},
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}
	if e.Link != "" {
		embed["url"] = e.Link
	}
	return map[string]any{"embeds": []any{embed}}
}

// slackPayload uses an attachment so the severity colour shows as the bar on
// the left. The `text` fallback is what a notification preview renders.
func slackPayload(e Event) map[string]any {
	var sb strings.Builder
	sb.WriteString("*" + e.Title + "*")
	if e.Summary != "" {
		sb.WriteString("\n" + e.Summary)
	}
	for _, f := range e.Fields {
		sb.WriteString("\n• *" + f.Label + ":* " + emptyDash(f.Value))
	}
	if e.Link != "" {
		sb.WriteString("\n<" + e.Link + "|Open in the admin panel>")
	}
	return map[string]any{
		"text": e.Title,
		"attachments": []any{
			map[string]any{
				"color":     severityColorHex(e.Severity),
				"text":      sb.String(),
				"mrkdwn_in": []string{"text"},
			},
		},
	}
}

func emailSubject(e Event) string {
	prefix := "[Warmbly]"
	if e.Severity == SeverityUrgent {
		prefix = "[Warmbly] Urgent:"
	}
	return prefix + " " + e.Title
}

// emailBodyHTML renders the alert. The transport derives the plain-text part
// from this, so it stays simple and table-free.
func emailBodyHTML(e Event) string {
	var sb strings.Builder
	sb.WriteString(`<div style="font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;font-size:14px;color:#0f172a;line-height:1.6">`)
	sb.WriteString(`<p style="margin:0 0 8px;font-size:16px;font-weight:600">` + esc(e.Title) + `</p>`)
	if e.Summary != "" {
		sb.WriteString(`<p style="margin:0 0 12px;color:#475569">` + esc(e.Summary) + `</p>`)
	}
	if len(e.Fields) > 0 {
		sb.WriteString(`<ul style="margin:0 0 12px;padding-left:18px;color:#334155">`)
		for _, f := range e.Fields {
			sb.WriteString(`<li><strong>` + esc(f.Label) + `:</strong> ` + esc(emptyDash(f.Value)) + `</li>`)
		}
		sb.WriteString(`</ul>`)
	}
	if e.Link != "" {
		sb.WriteString(`<p style="margin:0 0 12px"><a href="` + esc(e.Link) + `">Open in the admin panel</a></p>`)
	}
	sb.WriteString(`<p style="margin:16px 0 0;color:#94a3b8;font-size:12px">You are receiving this because this address is an operator notification channel on your Warmbly instance.</p>`)
	sb.WriteString(`</div>`)
	return sb.String()
}

func esc(v string) string { return html.EscapeString(v) }

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

func truncate(v string, max int) string {
	if len(v) <= max {
		return v
	}
	if max <= 1 {
		return v[:max]
	}
	return v[:max-1] + "…"
}
