package models

import "github.com/google/uuid"

type Pagination struct {
	Total      *int64     `json:"total"`
	NextCursor *uuid.UUID `json:"next_cursor"`
	HasMore    bool       `json:"has_more"`
}

type CPagination struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}
