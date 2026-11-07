package smtp

import (
	"errors"
	"fmt"
	"net/textproto"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/warmbly/warmbly/internal/errx"
)

var enhancedReply = regexp.MustCompile(`\b[245]\.[0-9]{1,3}\.[0-9]{1,3}\b`)

func sendFailure(base *errx.MailError, native error, stage, scope string) *errx.MailError {
	f := errx.SendFailure{Provider: "smtp", Protocol: "smtp", Status: replyCode(native),
		Stage: stage, Scope: scope, ObservedAt: time.Now().UTC(), Disposition: errx.SendRetry}
	var reply *textproto.Error
	if errors.As(native, &reply) {
		f.EnhancedStatus = enhancedReply.FindString(reply.Msg)
	}
	f.Cause = senderCause(native)
	switch {
	case f.Status >= 400 && f.Status < 500 || stage == "auth" && f.Status == 0:
		base = errx.ErrMailServerUnreachable
	case base.Type == errx.MailErrorCritical:
		f.Disposition = errx.SendAuth
	case f.Status >= 500:
		f.Disposition = errx.SendPermanent
	case f.Status == 0 && (stage == "message body" || stage == "message accept"):
		f.Disposition = errx.SendAmbiguous
	}
	result := errx.WithSendFailure(base, f)
	result.Message += fmt.Sprintf(" (SMTP %s status %d)", stage, f.Status)
	if f.EnhancedStatus != "" {
		result.Message += "; enhanced status " + f.EnhancedStatus
	}
	// An AUTH reply can echo a credential challenge, so only later stages quote the server.
	if reply != nil && stage != "auth" {
		if text := replyText(reply.Msg); text != "" {
			result.Message += fmt.Sprintf("; server replied: %d %s", reply.Code, text)
		}
	}
	return result
}

// replyTextLimit bounds the quoted reply in runes; it is external input.
const replyTextLimit = 300

var replyAddress = regexp.MustCompile(`[^\s<>"';:,()\[\]]+@[^\s<>"';:,()\[\]]+`)

// replyText is the server's own reply on one line: addresses masked, control
// characters dropped, whitespace collapsed, bounded.
func replyText(msg string) string {
	msg = replyAddress.ReplaceAllString(msg, "<address>")
	text := strings.Join(strings.FieldsFunc(msg, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(text); len(r) > replyTextLimit {
		text = string(r[:replyTextLimit]) + "…"
	}
	return text
}
