package models

import (
	"time"

	"github.com/google/uuid"
)

type Sequence struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`

	Subject   string `json:"subject"`
	BodyPlain string `json:"body_plain"`
	BodyHTML  string `json:"body_html"`
	BodySync  bool   `json:"body_sync"`
	BodyCode  bool   `json:"body_code"`

	WaitAfter int `json:"wait_after"`
	Position  int `json:"position"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type UpdateSequence struct {
	Name    *string `json:"name"`
	Subject *string `json:"subject"`

	BodyPlain *string `json:"body_plain"`
	BodyHTML  *string `json:"body_html"`
	BodySync  *bool   `json:"body_sync"`
	BodyCode  *bool   `json:"body_code"`

	WaitAfter *int `json:"wait_after"`
}
