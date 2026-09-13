package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The dashboard calls GET /subscription, which returns a bare Subscription,
// and decides whether the workspace is paid from this payload. `managed` lived
// only on SubscriptionWithLimits once, so the endpoint the dashboard actually
// uses never carried it and a granted plan stayed locked. Both shapes are
// pinned here.
func TestManagedIsOnEverySubscriptionPayload(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	planID := uuid.New()
	sub := Subscription{ManagedAt: &at, ManagedPlanID: &planID}
	sub.Managed = sub.IsManaged()

	for name, payload := range map[string]any{
		"GET /subscription":        sub,
		"GET /subscription/limits": SubscriptionWithLimits{Subscription: sub},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]any
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			got, present := out["managed"]
			if !present {
				t.Fatalf("`managed` is absent; a client cannot tell a granted plan from an unpaid one\n%s", raw)
			}
			if got != true {
				t.Errorf("`managed` = %v, want true", got)
			}
		})
	}
}

// Serialized unconditionally, not omitempty: a client reading `managed`
// as undefined would treat an unpaid workspace and a missing field alike,
// which is the failure this guards.
func TestManagedIsPresentEvenWhenFalse(t *testing.T) {
	raw, err := json.Marshal(Subscription{})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if got, present := out["managed"]; !present || got != false {
		t.Fatalf("want managed=false present, got %v (present=%v)\n%s", got, present, raw)
	}
}
