package models

import "github.com/google/uuid"

type JobEventHistoryIDUpdate struct {
	UserID    uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID   uuid.UUID `json:"email_id" avro:"email_id"`
	HistoryID uint64    `json:"history_id" avro:"history_id"`
}
