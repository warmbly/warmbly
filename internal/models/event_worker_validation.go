package models

import "github.com/google/uuid"

type EventWorkerEmailValidation struct {
	OrgID       uuid.UUID `json:"org_id"`
	ProcessID   uuid.UUID `json:"process_id"`
	Credentials *SmtpImap `json:"credentials"`
}
