package models

import "github.com/google/uuid"

type Order struct {
	ID       uuid.UUID `json:"id"`
	Position int32     `json:"position"`
}

type Move struct {
	Position int32 `json:"position"`
}
