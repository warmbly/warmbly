package models

import (
	"github.com/meszmate/apple-go"
	"github.com/meszmate/google-go"
)

type ExternalAuth struct {
	AppleAuth  apple.AppleAuth
	GoogleAuth *google.GoogleAuth
}
