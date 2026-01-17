package models

import (
	"time"

	"github.com/google/uuid"
)

type AuthType int

const (
	AuthPlain AuthType = iota
	AuthOAuth2
)

type JobEventTokenUpdate struct {
	UserID       uuid.UUID `json:"user_id"`
	EmailID      uuid.UUID `json:"email_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}
