package notify

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"time"
)

// buildMessage renders a complete RFC 5322 message. The SES path builds its own
// envelope, so this is the SMTP and log transports' shared formatter.
//
// It exists mostly for three things the old header string did not do: a Date
// and Message-ID header (mailbox providers penalize messages missing either),
// RFC 2047 encoding of non-ASCII display names and subjects, and a plain-text
// alternative alongside the HTML.
func buildMessage(fromName, fromAddress string, to, cc []string, replyTo, subject, html string) ([]byte, error) {
	if err := rejectHeaderInjection(subject, replyTo, fromName, fromAddress); err != nil {
		return nil, err
	}
	for _, addr := range append(append([]string{}, to...), cc...) {
		if err := rejectHeaderInjection(addr); err != nil {
			return nil, err
		}
	}

	boundary, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	messageID, err := newMessageID(fromAddress)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	writeHeader(&b, "From", formatAddress(fromName, fromAddress))
	writeHeader(&b, "To", strings.Join(to, ", "))
	if len(cc) > 0 {
		writeHeader(&b, "Cc", strings.Join(cc, ", "))
	}
	if replyTo != "" {
		writeHeader(&b, "Reply-To", replyTo)
	}
	writeHeader(&b, "Subject", mime.QEncoding.Encode("UTF-8", subject))
	writeHeader(&b, "Date", time.Now().Format(time.RFC1123Z))
	writeHeader(&b, "Message-ID", messageID)
	writeHeader(&b, "MIME-Version", "1.0")
	writeHeader(&b, "Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", boundary))
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
	b.WriteString(normalizeCRLF(htmlToPlainText(html)))
	b.WriteString("\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
	b.WriteString(normalizeCRLF(html))
	b.WriteString("\r\n\r\n")

	b.WriteString("--" + boundary + "--\r\n")

	return []byte(b.String()), nil
}

func writeHeader(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

func formatAddress(name, address string) string {
	if name == "" {
		return address
	}
	return fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("UTF-8", name), address)
}

// rejectHeaderInjection refuses values carrying a bare CR or LF. Subjects and
// display names reach here from user-controlled data (organization names, admin
// outreach), and a newline there splices arbitrary headers into the message.
func rejectHeaderInjection(values ...string) error {
	for _, v := range values {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("notify: header value contains a line break")
		}
	}
	return nil
}

func newMessageID(fromAddress string) (string, error) {
	token, err := randomToken(16)
	if err != nil {
		return "", err
	}
	domain := "localhost"
	if at := strings.LastIndex(fromAddress, "@"); at >= 0 && at+1 < len(fromAddress) {
		domain = fromAddress[at+1:]
	}
	return fmt.Sprintf("<%s@%s>", token, domain), nil
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// normalizeCRLF converts bare LF to CRLF as SMTP requires, without doubling the
// CR in text that already uses CRLF.
func normalizeCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

var (
	blockElements  = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6]|li|table|blockquote)>|<br\s*/?>`)
	anchorTag      = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	dropElements   = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)>`)
	anyTag         = regexp.MustCompile(`(?s)<[^>]*>`)
	blankLineRun   = regexp.MustCompile(`\n{3,}`)
	trailingSpaces = regexp.MustCompile(`[ \t]+\n`)
)

// htmlToPlainText produces the text/plain alternative. It keeps link targets
// inline, because the auth mails whose text part matters most are the ones
// carrying a reset or invite URL.
func htmlToPlainText(html string) string {
	s := dropElements.ReplaceAllString(html, "")
	s = anchorTag.ReplaceAllString(s, "$2 <$1>")
	s = blockElements.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")

	replacer := strings.NewReplacer(
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"\r", "",
	)
	s = replacer.Replace(s)

	s = trailingSpaces.ReplaceAllString(s, "\n")
	s = blankLineRun.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
