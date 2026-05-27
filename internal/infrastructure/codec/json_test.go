package codec

import (
	"context"
	"testing"
)

type jsonRoundTripPayload struct {
	ID    string   `json:"id"`
	Count int      `json:"count"`
	Tags  []string `json:"tags,omitempty"`
}

func TestJSONCodec_Name(t *testing.T) {
	if got := NewJSON().Name(); got != "json" {
		t.Fatalf("expected name 'json', got %q", got)
	}
}

func TestJSONCodec_RoundTrip(t *testing.T) {
	c := NewJSON()
	ctx := context.Background()

	in := jsonRoundTripPayload{ID: "abc", Count: 7, Tags: []string{"warmup", "premium"}}
	payload, err := c.Serialize(ctx, "any.topic", in)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("payload should not be empty")
	}

	var out jsonRoundTripPayload
	if err := c.Deserialize(ctx, "any.topic", payload, &out); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if out.ID != in.ID || out.Count != in.Count || len(out.Tags) != len(in.Tags) {
		t.Fatalf("round-trip mismatch: in=%+v out=%+v", in, out)
	}
	for i := range in.Tags {
		if in.Tags[i] != out.Tags[i] {
			t.Fatalf("tag mismatch at %d: %q vs %q", i, in.Tags[i], out.Tags[i])
		}
	}
}

func TestJSONCodec_TopicIgnored(t *testing.T) {
	c := NewJSON()
	ctx := context.Background()

	in := jsonRoundTripPayload{ID: "x", Count: 1}
	a, err := c.Serialize(ctx, "topic.one", in)
	if err != nil {
		t.Fatalf("serialize a: %v", err)
	}
	b, err := c.Serialize(ctx, "topic.two", in)
	if err != nil {
		t.Fatalf("serialize b: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("topic should not affect JSON output: %q vs %q", a, b)
	}
}

func TestJSONCodec_SerializeRejectsNil(t *testing.T) {
	c := NewJSON()
	if _, err := c.Serialize(context.Background(), "t", nil); err == nil {
		t.Fatal("expected error when serializing nil")
	}
}

func TestJSONCodec_DeserializeRejectsNilTarget(t *testing.T) {
	c := NewJSON()
	if err := c.Deserialize(context.Background(), "t", []byte(`{}`), nil); err == nil {
		t.Fatal("expected error when target is nil")
	}
}

func TestJSONCodec_DeserializeRejectsNonPointer(t *testing.T) {
	c := NewJSON()
	var target jsonRoundTripPayload
	// Pass by value: encoding/json refuses non-pointer targets.
	if err := c.Deserialize(context.Background(), "t", []byte(`{"id":"x"}`), target); err == nil {
		t.Fatal("expected error when target is not a pointer")
	}
}

func TestJSONCodec_DeserializeRejectsBadJSON(t *testing.T) {
	c := NewJSON()
	var target jsonRoundTripPayload
	if err := c.Deserialize(context.Background(), "t", []byte(`{not json`), &target); err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}
