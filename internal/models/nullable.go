package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// NullableTime distinguishes an absent JSON field from an explicit null in
// PATCH payloads. Absent leaves the column untouched; null clears it.
type NullableTime struct {
	Set   bool       `json:"-"`
	Value *time.Time `json:"-"`
}

func (n *NullableTime) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	return json.Unmarshal(b, &n.Value)
}

func (n NullableTime) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}

// NullableUUID distinguishes an absent JSON field from an explicit null in
// PATCH payloads. Absent leaves the column untouched; null clears it.
type NullableUUID struct {
	Set   bool       `json:"-"`
	Value *uuid.UUID `json:"-"`
}

func (n *NullableUUID) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	return json.Unmarshal(b, &n.Value)
}

func (n NullableUUID) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}

// NullableInt distinguishes an absent JSON field from an explicit null in
// PATCH payloads. Absent leaves the column untouched; null clears it.
type NullableInt struct {
	Set   bool `json:"-"`
	Value *int `json:"-"`
}

func (n *NullableInt) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	return json.Unmarshal(b, &n.Value)
}

func (n NullableInt) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}
