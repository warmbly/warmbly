package models

import (
	"github.com/meszmate/apple-go"
	"github.com/meszmate/google-go"
)

type ExternalAuth struct {
	AppleAuth  apple.AppleAuth
	GoogleAuth *google.GoogleAuth
}

// ExternalAuthProviders is what GET /auth/providers advertises so one shipped
// native app binary can discover which social sign-in options a (self-)hosted
// backend supports. Client IDs here are public identifiers, not secrets.
type ExternalAuthProviders struct {
	AppleBundleID     string
	GoogleIOSClientID string
}
