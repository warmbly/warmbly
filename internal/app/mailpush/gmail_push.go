package mailpush

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/warmbly/warmbly/internal/config"
	"google.golang.org/api/idtoken"
)

// GmailPushPath is the backend route a Pub/Sub push subscription posts Gmail watch notifications to.
const GmailPushPath = "/api/v1/mailbox-push/gmail"

var errGmailPushToken = errors.New("gmail push: request not signed by the configured service account")

// googleIssuers are the issuers Google signs Pub/Sub push tokens as.
var googleIssuers = map[string]bool{"accounts.google.com": true, "https://accounts.google.com": true}

type tokenValidator func(ctx context.Context, token, audience string) (*idtoken.Payload, error)

// GmailPush authenticates Pub/Sub push requests carrying Gmail watch notifications.
type GmailPush struct {
	audience string
	account  string
	validate tokenValidator
}

// NewGmailPush is nil unless GMAIL_PUSH_SERVICE_ACCOUNT names the account the
// push subscription signs with and the backend has a public HTTPS URL.
func NewGmailPush() *GmailPush {
	account := config.GmailPushServiceAccount()
	base := config.BackendPublicURL()
	if account == "" || !strings.HasPrefix(base, "https://") {
		return nil
	}
	return &GmailPush{audience: base + GmailPushPath, account: account, validate: idtoken.Validate}
}

// Verify checks the push request's bearer token: signed by Google, for this
// endpoint, from the configured service account, with a verified email.
func (g *GmailPush) Verify(ctx context.Context, authorization string) error {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || token == "" {
		return errGmailPushToken
	}
	payload, err := g.validate(ctx, token, g.audience)
	if err != nil {
		return errGmailPushToken
	}
	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	if !googleIssuers[payload.Issuer] || !verified || !strings.EqualFold(email, g.account) {
		return errGmailPushToken
	}
	return nil
}

// GmailPushAddress reads the mailbox address from a Pub/Sub push body.
func GmailPushAddress(body []byte) (string, bool) {
	var envelope struct {
		Message struct {
			Data string `json:"data"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		return "", false
	}
	return GmailAddress(data)
}
