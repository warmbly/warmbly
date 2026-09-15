package events

import (
	"encoding/json"
	"testing"
)

// The Rust tracking service writes this JSON and this package decodes it, on
// NATS directly and on Kafka whenever CODEC_PROVIDER is json. A rename on
// either side costs every open and click with nothing but a deserialize
// warning, so the names are pinned on both: tracking/src/events.rs asserts the
// keys it emits, and this asserts the keys we read. The two lists have to be
// the same list.
func TestTrackingEventDecodesEveryFieldTheEdgeWrites(t *testing.T) {
	wire := []byte(`{
		"event_type": "EMAIL_CLICKED",
		"task_id": "11111111-2222-3333-4444-555555555555",
		"original_url": "https://example.com/pricing",
		"link_id": "99999999-8888-7777-6666-555555555555",
		"timestamp": "2026-09-12T07:00:00Z",
		"user_agent": "Mozilla/5.0",
		"ip_hash": "deadbeef",
		"client_ip": "203.0.113.0",
		"scanner": "proofpoint",
		"scanner_probable": true
	}`)

	var got TrackingEvent
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.EventType != "EMAIL_CLICKED" || got.TaskID == "" || got.Timestamp == "" {
		t.Fatalf("scalar fields did not decode: %+v", got)
	}
	for name, ptr := range map[string]*string{
		"original_url": got.OriginalURL,
		"link_id":      got.LinkID,
		"user_agent":   got.UserAgent,
		"ip_hash":      got.IPHash,
		"client_ip":    got.ClientIP,
		"scanner":      got.Scanner,
	} {
		if ptr == nil || *ptr == "" {
			t.Errorf("%s decoded as nil or empty; the JSON name drifted", name)
		}
	}
	if !got.ScannerProbable {
		t.Error("scanner_probable decoded as false; the JSON name drifted")
	}
}

// Older tracking builds wrote fewer keys, and events written by them are still
// on the bus during an upgrade. Every one of them must decode to the
// conservative reading: no source recognised, and a recognised source treated
// as settling the verdict rather than merely widening the window.
func TestTrackingEventDecodesAnOlderEventConservatively(t *testing.T) {
	var got TrackingEvent
	if err := json.Unmarshal([]byte(`{
		"event_type": "EMAIL_OPENED",
		"task_id": "11111111-2222-3333-4444-555555555555",
		"timestamp": "2026-09-12T07:00:00Z",
		"user_agent": "Mozilla/5.0",
		"ip_hash": "deadbeef"
	}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Scanner != nil || got.ClientIP != nil || got.LinkID != nil {
		t.Errorf("absent fields must stay nil, got %+v", got)
	}
	if got.ScannerProbable {
		t.Error("an absent scanner_probable must read as false")
	}

	// And a labelled event from a build that predates the flag: the label
	// still decides on its own, which is the behaviour those builds had.
	got = TrackingEvent{}
	if err := json.Unmarshal([]byte(`{"event_type":"EMAIL_OPENED","task_id":"t","timestamp":"x","scanner":"microsoft-365-protection"}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Scanner == nil || got.ScannerProbable {
		t.Errorf("a pre-flag labelled event must be certain, got %+v", got)
	}
}
