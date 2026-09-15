package models

import (
	"reflect"
	"testing"

	"github.com/hamba/avro/v2"
)

// Every event the bus carries has to survive the codec. This is what makes the
// body registry a contract rather than a comment: a new event type whose body
// Avro cannot express fails here, on the machine that added it, instead of at
// the first send on a Kafka instance.
func TestEveryWorkerCommandRoundTrips(t *testing.T) {
	schema := WorkerEvent{}.Schema()
	for eventType, body := range WorkerEventBodies {
		t.Run(string(eventType), func(t *testing.T) {
			in := WorkerEvent{Type: eventType, Body: sample(body)}
			payload := encode(t, schema, in)
			var out WorkerEvent
			if err := avro.Unmarshal(schema, payload, &out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.Type != eventType {
				t.Fatalf("type = %q, want %q", out.Type, eventType)
			}
			assertBodyType(t, out.Body, body)
		})
	}
}

func TestEveryWorkerResultRoundTrips(t *testing.T) {
	schema := JobEvent{}.Schema()
	for eventType, body := range JobEventBodies {
		t.Run(string(eventType), func(t *testing.T) {
			in := JobEvent{Type: eventType, Body: sample(body)}
			payload := encode(t, schema, in)
			var out JobEvent
			if err := avro.Unmarshal(schema, payload, &out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.Type != eventType {
				t.Fatalf("type = %q, want %q", out.Type, eventType)
			}
			assertBodyType(t, out.Body, body)
		})
	}
}

func encode(t *testing.T, schema avro.Schema, in any) []byte {
	t.Helper()
	payload, err := avro.Marshal(schema, in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return payload
}

// The decoded body has to arrive as the type the handler asserts on. When it
// does not the consumer still works, by re-marshalling through JSON, so nothing
// else would notice the codec had quietly stopped paying for itself.
func assertBodyType(t *testing.T, got, want any) {
	t.Helper()
	if reflect.TypeOf(got) != reflect.TypeOf(sample(want)) {
		t.Fatalf("body decoded as %v, want %v", reflect.TypeOf(got), reflect.TypeOf(sample(want)))
	}
}

// A registry entry is a typed nil so it can name a type without holding a
// value. Sending one would encode nothing, so the test needs a real one.
func sample(body any) any {
	t := reflect.TypeOf(body)
	if t.Kind() == reflect.Pointer {
		return reflect.New(t.Elem()).Interface()
	}
	return reflect.New(t).Elem().Interface()
}

// Two bodies sharing a Go type name would collide in hamba's global type
// registry and decode as each other.
func TestBodyRecordNamesAreUnique(t *testing.T) {
	seen := map[string]reflect.Type{}
	check := func(body any) {
		bt := reflect.TypeOf(body)
		for bt.Kind() == reflect.Pointer {
			bt = bt.Elem()
		}
		if prev, ok := seen[bt.Name()]; ok && prev != bt {
			t.Fatalf("%v and %v share the record name %q", prev, bt, bt.Name())
		}
		seen[bt.Name()] = bt
	}
	for _, b := range WorkerEventBodies {
		check(b)
	}
	for _, b := range JobEventBodies {
		check(b)
	}
}
