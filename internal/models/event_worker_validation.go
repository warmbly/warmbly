package models

import "github.com/google/uuid"

type EventWorkerEmailValidation struct {
	UserID      uuid.UUID `json:"user_id"`
	ProcessID   uuid.UUID `json:"process_id"`
	Credentials *SmtpImap `json:"credentials"`
}
