package smtp

import "strings"

// authStatusCodes are enhanced status codes that mean, on their own, that the
// receiving server refused the mail because the SENDING DOMAIN failed its
// authentication bar.
var authStatusCodes = []string{
	"5.7.515", // Microsoft: does not meet the required authentication level
	"5.7.509", // Microsoft: does not pass DMARC verification
	"5.7.26",  // Google: unauthenticated email
}

// authPhrases name the sending domain's authentication explicitly enough to act
// on without a status code.
var authPhrases = []string{
	"does not meet the required authentication level",
	"unauthenticated email",
	"unauthenticated mail is not accepted",
	"dmarc policy",
	"spf check failed",
	"dkim signature",
}

// isDomainAuthRejection reports whether a refusal blames OUR sending domain's
// authentication rather than the recipient or the connection.
//
// Retrying one of these is pointless: every send from that domain fails
// identically until its DNS is fixed. Reading them as "server unreachable" is
// what made a broken SPF record look like an outage.
//
// A bare 5.7.1 does not count. It is the generic policy rejection and covers
// ordinary relay denials, so it only counts alongside one of the phrases.
func isDomainAuthRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	for _, code := range authStatusCodes {
		if strings.Contains(msg, code) {
			return true
		}
	}
	for _, phrase := range authPhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}
