package models

import "github.com/google/uuid"

// UserTOTP is a user's TOTP 2FA settings row (the secret is sealed at rest).
type UserTOTP struct {
	UserID       uuid.UUID
	SecretSealed string
	Enabled      bool
}

// RecoveryCode is one (argon2-hashed) single-use 2FA recovery code.
type RecoveryCode struct {
	ID       uuid.UUID
	CodeHash string
}
