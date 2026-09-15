package models

import (
	"reflect"
	"testing"
	"time"

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
// value. Sending one would encode nothing, so the test needs a real one, and
// every field in it has to be non-zero.
//
// Zero values are why this suite once passed over a uint64 mapped to Avro long:
// the encoder only refuses the conversion when there is something to convert.
// Populating every field is what makes the schema answer for the whole type
// rather than for the parts a zero value happens to exercise.
func sample(body any) any {
	t := reflect.TypeOf(body)
	if t.Kind() == reflect.Pointer {
		v := reflect.New(t.Elem())
		fill(v.Elem())
		return v.Interface()
	}
	v := reflect.New(t)
	fill(v.Elem())
	return v.Elem().Interface()
}

// fill writes a distinctive non-zero value into every exported field, walking
// nested structs, slices and maps. Unexported and avro:"-" fields are left
// alone: the schema does not describe them, so neither should this.
func fill(v reflect.Value) {
	if !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// The largest value the type holds, so a narrowing conversion shows up
		// as a wrong number rather than passing on a small one.
		v.SetUint((1 << (v.Type().Bits() - 1)) | 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte{1, 2, 3})
			return
		}
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0))
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		key := reflect.New(v.Type().Key()).Elem()
		fill(key)
		val := reflect.New(v.Type().Elem()).Elem()
		fill(val)
		v.SetMapIndex(key, val)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Unix(1750000000, 0).UTC()))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if f := v.Type().Field(i); f.IsExported() && f.Tag.Get("avro") != "-" {
				fill(v.Field(i))
			}
		}
	}
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
