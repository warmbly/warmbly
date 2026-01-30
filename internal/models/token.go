package models

import (
	"time"

	"github.com/google/uuid"
)

type Token struct {
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

type Session struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`

	// Current organization context for multi-org support
	CurrentOrganizationID *uuid.UUID `json:"current_organization_id,omitempty"`

	LocationCity        string `json:"location_city"`
	LocationRegion      string `json:"location_region"`
	LocationCountry     string `json:"location_country"`
	LocationCountryCode string `json:"location_country_code"`
	LocationPostalCode  string `json:"location_postal_code"`

	BrowserName string `json:"browser_name"`
	OSName      string `json:"os_name"`

	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	ExpiresAt *time.Time `json:"expires_at"`

	LastRefreshedAt time.Time `json:"last_refreshed_at"`
	RefreshNonce    string    `json:"refresh_nonce"`
	AccessNonce     string    `json:"access_nonce"`
}

type AuthSession struct {
	Session string `json:"session"`
}

type LoginSession struct {
	CodeHash string `json:"code_hash"`
	Tries    int    `json:"tries"`
	Nonce    string `json:"nonce"`
}

type RegistrationSession struct {
	PasswordHash string `json:"password_hash"`
	CodeHash     string `json:"code_hash"`
	Tries        int    `json:"tries"`
	Nonce        string `json:"nonce"`
}
