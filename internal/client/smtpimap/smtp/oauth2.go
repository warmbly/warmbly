package smtp

import (
	"fmt"
	"net/smtp"
)

// ---------- XOAUTH2 Implementation ----------

type oauth2Auth struct {
	username, accessToken string
}

func newOAuth2Auth(user, token string) smtp.Auth {
	return &oauth2Auth{user, token}
}

func (a *oauth2Auth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	payload := fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", a.username, a.accessToken)
	return "XOAUTH2", []byte(payload), nil
}

func (a *oauth2Auth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, fmt.Errorf("unexpected server challenge: %s", string(fromServer))
	}
	return nil, nil
}
