package models

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/hamba/avro/v2"
)

func TestObservationMetricBoundaries(t *testing.T) {
	for _, tc := range []struct{ n, d, missing int }{{0, 0, 0}, {1, 0, 1}, {-1, 1, 0}, {2, 1, 0}, {0, -1, 0}, {1, 1, -1}} {
		m := NewObservationMetric("seed_panel", "classified", "fraction", tc.n, tc.d, tc.missing)
		if m.Value != nil || m.Interval != nil {
			t.Fatalf("invalid or empty denominator acquired value: %+v", m)
		}
	}
	for _, unit := range []string{"fraction", "percent"} {
		scale := 1.0
		if unit == "percent" {
			scale = 100
		}
		for _, n := range []int{0, 1} {
			m := NewObservationMetric("seed_panel", "classified", unit, n, 1, 3)
			if m.Value == nil || *m.Value != float64(n)*scale || m.Interval == nil || m.Interval.Lower < 0 || m.Interval.Upper > scale || math.IsNaN(m.Interval.Lower) || m.Interval.Upper <= m.Interval.Lower || m.Unresolved != 3 {
				t.Fatalf("single observation interval: %+v", m)
			}
		}
	}
}

func TestEvidenceFlagCompatibilityAndTrust(t *testing.T) {
	var old EmailMessageStoreData
	if err := json.Unmarshal([]byte(`{"folder":"inbox","flags":["\\Seen"]}`), &old); err != nil {
		t.Fatal(err)
	}
	if got := EvidenceFromFlags(old.Flags); got.Trust != "unknown" || got.SPF != "unknown" {
		t.Fatal(got)
	}
	forged := UnknownReceivedEvidence()
	forged.Source, forged.Trust, forged.SPF, forged.DKIM, forged.DMARC, forged.TLS, forged.Alignment = "gmail_api", "trusted", "pass", "pass", "pass", "pass", "pass"
	flags := WithReceivedEvidence([]string{"SPAM", forged.Flag()}, forged)
	if len(flags) != 2 || !HasSpamFlag(flags) {
		t.Fatal("duplicate metadata or spam semantics changed")
	}
	got := EvidenceFromFlags(flags)
	if got.Trust != "unverified_headers" || got.SPF != "unknown" || got.DKIM != "unknown" || got.DMARC != "unknown" || got.TLS != "unknown" || got.Alignment != "unknown" {
		t.Fatal("forged trust accepted")
	}
	if len(PlacementObservationFlags([]string{"SPAM", "CATEGORY_PROMOTIONS", "Token:secret", forged.Flag()})) != 2 {
		t.Fatal("observation retained raw header values")
	}
}

func TestReceivedMetadataSurvivesExistingJSONAndAvroEvents(t *testing.T) {
	for _, flags := range [][]string{{"\\Seen"}, {"SPAM", UnknownReceivedEvidence().Flag(), ObservationUnknownFolderFlag}} {
		body := &JobEventNewEmail{Message: &EmailMessageStoreData{Flags: flags}}
		schema := JobEvent{}.Schema()
		in := JobEvent{Type: JobEventTypeNewEmail, Body: body}
		payload, err := avro.Marshal(schema, in)
		if err != nil {
			t.Fatal(err)
		}
		var out JobEvent
		if err := avro.Unmarshal(schema, payload, &out); err != nil {
			t.Fatal(err)
		}
		assertBody(t, out.Body, body)
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		var decoded JobEventNewEmail
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatal(err)
		}
		if EvidenceFromFlags(decoded.Message.Flags).SPF != "unknown" {
			t.Fatal("old or new JSON upgraded authentication")
		}
	}
}

func TestReceiptAndSubmissionMetricsStaySeparate(t *testing.T) {
	w := WarmupPlacementCounts{Sent: 10, Inbox: 1, Spam: 1, Unknown: 2, Archived: 1, Custom: 1, Instrumented: 6, Rescued: 1, Unconfirmed: 4}
	w.Finish()
	if w.Delivered != 2 || w.Observed != 6 || w.NonSpamMetric.Denominator != 2 || *w.InboxRate != 50 || w.RescueRequested != 1 || w.RescueConfirmed != nil {
		t.Fatalf("warmup evidence mixed: %+v", w)
	}
	var seed PlacementCounts
	for _, f := range []string{"inbox", "missing", "unknown", "archive", "custom", "pending"} {
		seed.Add(f)
	}
	seed.Finish()
	if seed.Delivered != 2 || *seed.InboxRate != .5 || seed.Observed != 4 || seed.Classified != 1 || seed.Unresolved != 2 || seed.PrimaryMetric.Denominator != 1 || *seed.PrimaryMetric.Value != 1 {
		t.Fatalf("legacy units or new evidence basis changed: %+v", seed)
	}
	for _, f := range []string{"", "My Folder", "InBoX", "SpAm", "archive"} {
		warm, placement := ClassifyWarmupLanding(f, nil), ClassifyPlacementLanding(f, nil)
		if (f == "" || f == "My Folder" || f == "archive") && (warm == WarmupLandedInbox || placement == PlacementFolderInbox) {
			t.Fatalf("unclassified folder became inbox: %q", f)
		}
	}
}
