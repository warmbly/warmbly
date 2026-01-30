package models

import (
	"time"

	"github.com/google/uuid"
)

type Group struct {
	ID uuid.UUID `json:"id"`

	Title    string `json:"title"`
	Color    string `json:"color"`
	Position int32  `json:"position"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GroupUpdate struct {
	Title *string `json:"title"`
	Color *string `json:"color"`
}

type GroupCreate struct {
	Title string `json:"title"`
	Color string `json:"color"`
}

type GroupType string

const Tags GroupType = "tags"
const Categories GroupType = "categories"
const Folders GroupType = "folders"
