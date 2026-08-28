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
// authentication rather than the recipient or the connection. A bare 5.7.1 is
// deliberately not enough: it covers ordinary relay denials too.
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
