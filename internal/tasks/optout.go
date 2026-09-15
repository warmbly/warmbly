package tasks

import (
	"context"
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/unsublink"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

// UnsubscribeLinkVar is the template variable a step can place by hand
// ({{.UnsubscribeLink}}); it resolves to the recipient's own opt-out link.
const UnsubscribeLinkVar = "UnsubscribeLink"

// unsubscribePathMarker is the path segment every minted link carries, on the
// API origin and on a workspace's own tracking domain alike. Click tracking
// leaves such links alone so an opt-out is never counted as a click or bounced
// through a redirect.
const unsubscribePathMarker = unsublink.Path

// mintUnsubscribeLink returns the recipient's opt-out address for a campaign:
// a short stored ticket when one can be written, and the long signed link
// when it cannot. Length is the whole reason the ticket exists (the
// text/plain alternative prints this address in full, issue #498), and a
// signed link is still a working opt-out, so a store failure degrades the
// address rather than the mechanism.
//
// The ticket is per recipient per campaign and reused, so every step of a
// sequence carries the same address.
func (s *tasksService) mintUnsubscribeLink(ctx context.Context, origin string, orgID, campaignID, contactID uuid.UUID) string {
	now := time.Now()
	if s.unsubTickets != nil {
		if token, err := unsublink.NewTicket(); err == nil {
			stored, err := s.unsubTickets.Mint(ctx, token, orgID, campaignID, contactID, now.Add(unsublink.Validity))
			if err == nil {
				return s.unsubLinks.TicketURL(origin, stored)
			}
			log.Warn().Err(err).
				Str("campaign_id", campaignID.String()).
				Msg("unsubscribe ticket mint failed; sending the signed link")
		}
	}
	return s.unsubLinks.URLOn(origin, orgID, campaignID, contactID, now)
}

// optOutFooter renders the in-body opt-out for one recipient, as HTML and as
// plain text, or empty strings when the effective mode is off. Link mode with
// no link available (the instance has no public API URL) falls back to the
// text line so a recipient is never left without a way out.
func optOutFooter(settings models.UnsubscribeSettings, linkURL string) (htmlPart, plainPart string) {
	switch settings.Mode {
	case models.UnsubscribeModeOff:
		return "", ""
	case models.UnsubscribeModeLink:
		if linkURL == "" {
			break
		}
		intro := strings.TrimSpace(settings.LinkIntro)
		text := strings.TrimSpace(settings.LinkText)
		htmlPart = `<p style="font-size:12px;color:#64748b;margin-top:16px">` +
			html.EscapeString(intro) + ` <a href="` + html.EscapeString(linkURL) + `" style="color:#64748b">` + html.EscapeString(text) + `</a></p>`
		plainPart = intro + " " + text + ": " + linkURL
		return htmlPart, strings.TrimSpace(plainPart)
	}
	line := strings.TrimSpace(settings.Text)
	if line == "" {
		return "", ""
	}
	return `<p style="font-size:12px;color:#64748b;margin-top:16px">` + html.EscapeString(line) + `</p>`, line
}

// appendOptOut adds the footer after everything else (signature included) so
// it sits where a reader expects an opt-out: last, and inside the container
// the email was laid out in rather than under it (issue #462).
func appendOptOut(bodyHTML, bodyPlain string, settings models.UnsubscribeSettings, linkURL string) (string, string) {
	htmlPart, plainPart := optOutFooter(settings, linkURL)
	if htmlPart == "" {
		return bodyHTML, bodyPlain
	}
	if bodyHTML != "" {
		bodyHTML = mailhtml.AppendToContent(bodyHTML, htmlPart)
	}
	if bodyPlain != "" {
		bodyPlain += "\n\n" + plainPart
	}
	return bodyHTML, bodyPlain
}

// linkifyUnsubscribeURL turns a hand-placed {{.UnsubscribeLink}} into a real
// link. The variable resolves to the recipient's signed URL, so a token
// dropped into prose ships as the bare API address (issue #341); this wraps
// every loose occurrence in an anchor labelled with the workspace's
// unsubscribe link text instead. A URL the author already put in an
// attribute (their own <a href>) is inside a tag and left untouched, and one
// sitting as the text of an existing anchor becomes that anchor's label
// rather than a nested link.
func linkifyUnsubscribeURL(bodyHTML, linkURL, linkText string) string {
	if bodyHTML == "" || linkURL == "" || !strings.Contains(bodyHTML, linkURL) {
		return bodyHTML
	}
	label := html.EscapeString(strings.TrimSpace(linkText))
	if label == "" {
		label = models.DefaultUnsubscribeLinkText
	}
	anchor := `<a href="` + html.EscapeString(linkURL) + `">` + label + `</a>`

	var b strings.Builder
	b.Grow(len(bodyHTML) + len(anchor))
	depth := 0 // open <a> elements around the current text node
	for i := 0; i < len(bodyHTML); {
		if bodyHTML[i] == '<' {
			end := mailhtml.TagEnd(bodyHTML[i:])
			if end < 0 {
				b.WriteString(bodyHTML[i:]) // unterminated tag: copy the rest verbatim
				break
			}
			tag := bodyHTML[i : i+end+1]
			switch {
			case isTagStart(tag, "a"):
				depth++
			case isTagStart(tag, "/a"):
				if depth > 0 {
					depth--
				}
			}
			b.WriteString(tag)
			i += end + 1
			continue
		}
		stop := len(bodyHTML)
		if next := strings.IndexByte(bodyHTML[i:], '<'); next >= 0 {
			stop = i + next
		}
		with := anchor
		if depth > 0 {
			with = label
		}
		b.WriteString(strings.ReplaceAll(bodyHTML[i:stop], linkURL, with))
		i = stop
	}
	return b.String()
}

// isTagStart reports whether tag (a full "<...>" slice) is the named tag,
// case-insensitively: isTagStart(`<A HREF="x">`, "a") and
// isTagStart("</A>", "/a") are both true.
func isTagStart(tag, name string) bool {
	if len(tag) < len(name)+2 || !strings.EqualFold(tag[1:1+len(name)], name) {
		return false
	}
	switch c := tag[1+len(name)]; c {
	case '>', '/', ' ', '\t', '\n', '\r', '\f':
		return true
	}
	return false
}
