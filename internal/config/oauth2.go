package config

import "golang.org/x/oauth2"

type Oauth2 struct {
	GoogleAuthorization *oauth2.Config
	InboxAuthorization  Oauth2Inbox
}

func LoadOauth2(baseURL string) *Oauth2 {
	return &Oauth2{
		GoogleAuthorization: GoogleOauth2Auth(baseURL),
		InboxAuthorization:  LoadOauth2Inbox(baseURL),
	}
}

func LoadOauth2Inbox(baseURL string) Oauth2Inbox {
	return Oauth2Inbox{
		Google:  GoogleOauth2Inbox(baseURL),
		Outlook: OutlookOauth2Inbox(baseURL),
	}
}
