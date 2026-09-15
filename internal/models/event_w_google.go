package models

import "github.com/google/uuid"

type JobEventHistoryIDUpdate struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	// int64, not uint64: Avro has no unsigned 64-bit type, and a Gmail history
	// id is many orders of magnitude below the point where the two differ.
	// JSON still writes a plain number, so both codecs read the same wire.
	HistoryID int64 `json:"history_id"`
}
