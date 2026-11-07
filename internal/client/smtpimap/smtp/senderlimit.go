package smtp

import (
	"errors"
	"net/textproto"
	"strings"

	"github.com/warmbly/warmbly/internal/errx"
)

// senderPhrases name a quota or restriction on the sending account in a reply whose status code alone is generic; first match wins.
var senderPhrases = []struct{ phrase, cause string }{
	{"rate limit", errx.SendCauseRateLimited},
	{"submissionquotaexceeded", errx.SendCauseSendingLimit},
	{"submission quota", errx.SendCauseSendingLimit},
	{"sending limit", errx.SendCauseSendingLimit},
	{"sending quota", errx.SendCauseSendingLimit},
	{"daily limit", errx.SendCauseSendingLimit},
	{"exceededmessagelimit", errx.SendCauseSendingLimit},
	{"outboundspamexception", errx.SendCauseSenderBlocked},
	{"unusual sending activity", errx.SendCauseSenderBlocked},
	{"restricted from sending", errx.SendCauseSenderBlocked},
	{"blocked from sending", errx.SendCauseSenderBlocked},
	{"sending has been disabled", errx.SendCauseSenderBlocked},
}

// senderCause reads a reply's text for a refusal of the sending account rather than of a recipient.
func senderCause(err error) string {
	var reply *textproto.Error
	if !errors.As(err, &reply) {
		return ""
	}
	msg := strings.ToLower(reply.Msg)
	for _, p := range senderPhrases {
		if strings.Contains(msg, p.phrase) {
			return p.cause
		}
	}
	return ""
}

// senderRefusal is a refusal at RCPT that is about the sending account, so it must not be read as a bounce.
func senderRefusal(err error) bool {
	if senderCause(err) != "" {
		return true
	}
	var reply *textproto.Error
	return errors.As(err, &reply) && errx.SenderPauseStatus(enhancedReply.FindString(reply.Msg))
}
