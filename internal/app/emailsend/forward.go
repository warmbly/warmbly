package emailsend

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

const forwardSeparator = "---------- Forwarded message ---------"

// forwardDateLayout is Gmail's forward header date, plus the zone it is read in.
const forwardDateLayout = "Mon, Jan 2, 2006 at 3:04 PM MST"

// bodyTag matches the <body> tags sanitizing keeps. Nested in the forward, a
// parser would merge its attributes onto the whole email's body.
var bodyTag = regexp.MustCompile(`(?i)<(/?)body\b`)

// renderForwarded renders the message a forward carries in the shape Gmail
// writes: a separator, the original envelope, then the original body.
func renderForwarded(m *models.ForwardedMessage, loc *time.Location) (htmlBody, plain string) {
	type field struct{ name, value string }
	fields := []field{{"From", strings.Join(m.From, ", ")}}
	if !m.Date.IsZero() {
		fields = append(fields, field{"Date", m.Date.In(loc).Format(forwardDateLayout)})
	}
	fields = append(fields, field{"Subject", m.Subject}, field{"To", strings.Join(m.To, ", ")})
	if len(m.CC) > 0 {
		fields = append(fields, field{"Cc", strings.Join(m.CC, ", ")})
	}

	bodyPlain := strings.TrimSpace(m.BodyPlain)
	if bodyPlain == "" {
		bodyPlain = mailhtml.ToPlainText(m.BodyHTML)
	}
	// Inlined before sanitizing, which drops <style>, so the original keeps its look.
	bodyHTML := bodyTag.ReplaceAllString(mailhtml.Sanitize(mailhtml.InlineCSS(m.BodyHTML)), "<${1}div")
	if strings.TrimSpace(bodyHTML) == "" {
		bodyHTML = mailhtml.FromText(bodyPlain)
	}

	var p, h strings.Builder
	p.WriteString(forwardSeparator + "\n")
	h.WriteString(`<div class="gmail_quote gmail_quote_container"><div dir="ltr" class="gmail_attr">`)
	h.WriteString(forwardSeparator + "<br>")
	for _, f := range fields {
		p.WriteString(f.name + ": " + f.value + "\n")
		h.WriteString(f.name + ": " + html.EscapeString(f.value) + "<br>")
	}
	p.WriteString("\n" + bodyPlain)
	h.WriteString("</div><br><br>" + bodyHTML + "</div>")
	return h.String(), p.String()
}

// forwardNote gives a forward's note both parts. The forwarded message ships
// as HTML and text, so a note written in only one would vanish from the other.
func forwardNote(bodyHTML, bodyPlain string) (string, string) {
	if !mailhtml.HasContent(bodyHTML) && strings.TrimSpace(bodyPlain) != "" {
		bodyHTML = mailhtml.FromText(bodyPlain)
	}
	if strings.TrimSpace(bodyPlain) == "" && mailhtml.HasContent(bodyHTML) {
		bodyPlain = mailhtml.ToPlainText(bodyHTML)
	}
	return bodyHTML, bodyPlain
}

// mailboxLocation is the zone a mailbox's clock reads in, UTC when unset or unknown.
func mailboxLocation(account *models.Email) *time.Location {
	if tz := account.ClockTimezone(); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}

// clickTicket matches a Warmbly click ticket on any tracking host.
var clickTicket = regexp.MustCompile(`https?://[^\s"'<>/]+/c/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// maxForwardedTickets bounds the ticket lookups one forward can cost.
const maxForwardedTickets = 64

// untrackForwarded swaps click tickets in a forwarded Warmbly send back to
// their destinations, so the new recipient's clicks are not credited to the
// original send. Unknown or expired tickets stay as they are.
func (s *emailSendService) untrackForwarded(ctx context.Context, htmlBody, plain string) (string, string) {
	if s.trackedLinkRepo == nil {
		return htmlBody, plain
	}
	found := clickTicket.FindAllStringSubmatch(htmlBody+"\n"+plain, -1)
	dest := make(map[string]string, len(found))
	for _, m := range found {
		if _, seen := dest[m[0]]; seen || len(dest) >= maxForwardedTickets {
			continue
		}
		dest[m[0]] = ""
		id, err := uuid.Parse(m[1])
		if err != nil {
			continue
		}
		link, err := s.trackedLinkRepo.GetByID(ctx, id)
		if err != nil || link == nil {
			continue
		}
		if u, err := url.Parse(link.Destination); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			dest[m[0]] = link.Destination
		}
	}
	for ticket, to := range dest {
		if to == "" {
			continue
		}
		htmlBody = strings.ReplaceAll(htmlBody, ticket, html.EscapeString(to))
		plain = strings.ReplaceAll(plain, ticket, to)
	}
	return htmlBody, plain
}
