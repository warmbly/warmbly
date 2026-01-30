package models

import "github.com/google/uuid"

type Log struct {
	WorkerID uuid.UUID `json:"worker_id"`
}
