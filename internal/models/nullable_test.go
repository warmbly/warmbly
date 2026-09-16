package models

import (
	"encoding/json"
	"testing"
	"time"
)

// Issue #171: PATCH /campaigns must distinguish an absent start_date (leave
// untouched) from an explicit null (clear it / start now).
func TestNullableTimeAbsentVsNullVsValue(t *testing.T) {
	var u UpdateCampaign
	if err := json.Unmarshal([]byte(`{}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.StartDate.Set {
		t.Fatal("absent field must not be marked Set")
	}

	u = UpdateCampaign{}
	if err := json.Unmarshal([]byte(`{"start_date":null}`), &u); err != nil {
		t.Fatal(err)
	}
	if !u.StartDate.Set || u.StartDate.Value != nil {
		t.Fatalf("explicit null must be Set with nil Value, got Set=%v Value=%v", u.StartDate.Set, u.StartDate.Value)
	}

	u = UpdateCampaign{}
	if err := json.Unmarshal([]byte(`{"start_date":"2030-01-02T00:00:00Z"}`), &u); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	if !u.StartDate.Set || u.StartDate.Value == nil || !u.StartDate.Value.Equal(want) {
		t.Fatalf("value must round-trip, got Set=%v Value=%v", u.StartDate.Set, u.StartDate.Value)
	}
}

func TestUpdateCampaignTouchesSchedule(t *testing.T) {
	var u UpdateCampaign
	if err := json.Unmarshal([]byte(`{"name":"x"}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.TouchesSchedule() {
		t.Fatal("a name-only patch must not touch the schedule")
	}
	if err := json.Unmarshal([]byte(`{"start_date":null}`), &u); err != nil {
		t.Fatal(err)
	}
	if !u.TouchesSchedule() {
		t.Fatal("clearing start_date must count as a schedule change")
	}
}

// PATCH /admin/discounts has the same absent-vs-null problem: an operator
// lifting an expiry or uncapping redemptions sends an explicit null, and an
// omitted key has to keep meaning "leave it alone".
//
// The wrappers must not be declared as pointers. encoding/json sets a settable
// outer pointer to nil for a JSON null and never calls UnmarshalJSON, so a
// *NullableInt reports Set=false for the one input it exists to recognise and
// the field silently cannot be cleared.
func TestUpdateDiscountNullableCaps(t *testing.T) {
	var u UpdateDiscountCodeRequest
	if err := json.Unmarshal([]byte(`{"description":"x"}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.MaxRedemptions.Set || u.StartsAt.Set || u.ExpiresAt.Set {
		t.Fatal("absent caps must not be marked Set")
	}

	u = UpdateDiscountCodeRequest{}
	body := `{"max_redemptions":null,"starts_at":null,"expires_at":null}`
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		t.Fatal(err)
	}
	if !u.MaxRedemptions.Set || u.MaxRedemptions.Value != nil {
		t.Fatalf("null max_redemptions must be Set with nil Value, got Set=%v Value=%v",
			u.MaxRedemptions.Set, u.MaxRedemptions.Value)
	}
	if !u.StartsAt.Set || u.StartsAt.Value != nil {
		t.Fatalf("null starts_at must be Set with nil Value, got Set=%v", u.StartsAt.Set)
	}
	if !u.ExpiresAt.Set || u.ExpiresAt.Value != nil {
		t.Fatalf("null expires_at must be Set with nil Value, got Set=%v", u.ExpiresAt.Set)
	}

	u = UpdateDiscountCodeRequest{}
	if err := json.Unmarshal([]byte(`{"max_redemptions":250}`), &u); err != nil {
		t.Fatal(err)
	}
	if !u.MaxRedemptions.Set || u.MaxRedemptions.Value == nil || *u.MaxRedemptions.Value != 250 {
		t.Fatalf("a value must round-trip, got Set=%v Value=%v", u.MaxRedemptions.Set, u.MaxRedemptions.Value)
	}
}
