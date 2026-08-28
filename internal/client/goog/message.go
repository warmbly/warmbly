package goog

import (
	"encoding/base64"
	"fmt"
	stdhtml "html"
	"net/mail"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
	"google.golang.org/api/gmail/v1"
)

func getAddressList(headers []*gmail.MessagePartHeader, name string) []string {
	var result []string
	for _, h := range headers {
		if h.Name == name && h.Value != "" {
			// Parse multiple addresses in the header
			addrs, err := mail.ParseAddressList(h.Value)
			if err != nil {
				// fallback: just split by comma
				for v := range strings.SplitSeq(h.Value, ",") {
					result = append(result, mailhdr.DecodeWords(strings.TrimSpace(v)))
				}
				continue
			}

			// Format as "Name <email@example.com>"
			for _, addr := range addrs {
				if addr.Name != "" {
					result = append(result, fmt.Sprintf("%s <%s>", addr.Name, addr.Address))
				} else {
					result = append(result, addr.Address)
				}
			}
		}
	}
	return result
}

// getSingleHeader returns a header value with RFC 2047 encoded-words decoded.
// Gmail's API hands headers back raw, so a non-ASCII subject arrives as
// "=?utf-8?q?...?=" and would be shown that way.
func getSingleHeader(headers []*gmail.MessagePartHeader, name string) string {
	for _, h := range headers {
		// RFC 5322 header names are case-insensitive; match accordingly so the
		// warmup token (and other headers) resolve regardless of provider casing.
		if strings.EqualFold(h.Name, name) {
			return mailhdr.DecodeWords(h.Value)
		}
	}
	return ""
}

// reportPartTypes are the machine-readable parts of a multipart/report. They
// carry the whole point of the message — a bounce's Status and returned
// headers, a feedback report's Feedback-Type and reported Message-ID — and are
// plain text on the wire. Taking only text/plain and text/html left every one
// of them behind, so the DSN and ARF parsers saw the human notice and nothing
// else.
var reportPartTypes = map[string]bool{
	"message/delivery-status": true,
	"message/feedback-report": true,
	"message/rfc822":          true,
	"message/rfc822-headers":  true,
	"text/rfc822-headers":     true,
}

func extractBody(parts []*gmail.MessagePart) (plain, html string) {
	for _, p := range parts {
		if p == nil {
			continue
		}
		hasData := p.Body != nil && p.Body.Data != ""
		switch {
		case p.MimeType == "text/plain" && hasData:
			plain += decodeBase64URL(p.Body.Data)
		case p.MimeType == "text/html" && hasData:
			html += decodeBase64URL(p.Body.Data)
		case reportPartTypes[p.MimeType] && hasData:
			plain += "\n" + decodeBase64URL(p.Body.Data)
		case len(p.Parts) > 0:
			pPlain, pHTML := extractBody(p.Parts)
			plain += pPlain
			html += pHTML
		}
	}
	return
}

// decodeBase64URL decodes Gmail body data, which is base64url and usually
// unpadded (padded URLEncoding rejects it, which silently emptied bodies).
func decodeBase64URL(data string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(data, "="))
	if err != nil {
		return ""
	}
	return string(decoded)
}

func parseGmailDate(dateText string) time.Time {
	date, err := mail.ParseDate(dateText)
	if err != nil {
		return time.Time{}
	}
	return date
}

func GmailMessageToEmailData(msg *gmail.Message) *models.EmailMessageData {
	var headers []*gmail.MessagePartHeader
	if msg.Payload != nil {
		headers = msg.Payload.Headers
	}

	// Walk from the payload itself, not payload.Parts: single-part messages
	// (most plaintext mail) carry their body directly on the payload and have
	// no parts at all.
	plain, html := extractBody([]*gmail.MessagePart{msg.Payload})

	return &models.EmailMessageData{
		GmailID:  msg.Id,
		UID:      0, // Gmail has no IMAP UID
		ThreadID: msg.ThreadId,
		Flags: func() []string {
			flags := []string{}
			// Gmail models read state inversely: the UNREAD label is present on
			// unread mail, so \Seen applies only when UNREAD is absent.
			seen := true
			for _, label := range msg.LabelIds {
				switch label {
				case "UNREAD":
					seen = false
				case "STARRED":
					flags = append(flags, "\\Flagged")
				case "IMPORTANT":
					flags = append(flags, "\\Important")
				case "DRAFT":
					flags = append(flags, "\\Draft")
				case "SPAM":
					// Drives warmup spam-placement detection and placement
					// tests (containsSpamFlag matches "SPAM").
					flags = append(flags, "SPAM")
				default:
					// CATEGORY_* tab labels feed placement classification
					// (promotions tab detection).
					if strings.HasPrefix(label, "CATEGORY_") {
						flags = append(flags, label)
					}
				}
			}
			if seen {
				flags = append(flags, "\\Seen")
			}
			// Surface the warmup verification token as a pseudo-flag so the
			// consumer can categorize warmup mail into the Warmbly folder.
			if tok := getSingleHeader(headers, config.WarmupVerifyHeader); tok != "" {
				flags = append(flags, config.WarmupVerifyHeader+":"+tok)
			}
			// Surface machine-reply / DSN-bounce markers so the consumer's
			// reply/bounce classifier can read them.
			for _, name := range config.InboundClassificationHeaders {
				if v := strings.TrimSpace(getSingleHeader(headers, name)); v != "" {
					flags = append(flags, name+":"+v)
				}
			}
			return flags
		}(),
		BCC:          getAddressList(headers, "Bcc"),
		CC:           getAddressList(headers, "Cc"),
		Date:         parseGmailDate(getSingleHeader(headers, "Date")),
		From:         getAddressList(headers, "From"),
		InReplyTo:    getAddressList(headers, "In-Reply-To"),
		MessageID:    getSingleHeader(headers, "Message-ID"),
		ReplyTo:      getAddressList(headers, "Reply-To"),
		Sender:       getAddressList(headers, "Sender"),
		Subject:      getSingleHeader(headers, "Subject"),
		To:           getAddressList(headers, "To"),
		Size:         msg.SizeEstimate,
		InternalDate: time.Unix(msg.InternalDate/1000, 0),
		ModSeq:       msg.HistoryId,
		// Gmail hands the snippet back HTML-escaped ("it&#39;s"), so decode it
		// or the conversation list shows the entity codes.
		Snippet:   stdhtml.UnescapeString(msg.Snippet),
		BodyPlain: plain,
		BodyHTML:  html,
	}
}
