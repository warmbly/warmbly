package models

import (
	"encoding"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/hamba/avro/v2"
)

// Avro has to be told the shape of a union before it meets one, and the two bus
// envelopes carry a body whose type depends on a discriminator. This derives
// each envelope's schema from the body registry rather than from a checked-in
// .avsc, so a field added to a payload cannot drift from the schema the registry
// holds: both come from the same struct.
//
// The serializer prefers a Schema() method over reflecting on the value it was
// handed, which is the only reason an `any` body can be encoded at all:
// reflection alone sees an interface and has nothing to describe.
//
// The walk below is ours rather than the one the Confluent serializer ships,
// for two reasons that are really one. It skips unexported fields, and it
// honours `avro:"-"` before descending rather than after. The upstream walk
// does neither, so oauth2.Token's unexported `raw any` and the already-excluded
// oauth2.TokenSource both made the schema unbuildable. Doing it this way also
// means the schema describes exactly the fields encoding/json puts on the wire
// today, which is what makes the two codecs interchangeable during a cutover.

var (
	eventSchemaOnce   sync.Once
	workerEventSchema avro.Schema
	jobEventSchema    avro.Schema
)

// Schema lets the Avro serializer encode a worker command without reflecting
// over its interface-typed body.
func (WorkerEvent) Schema() avro.Schema { buildEventSchemas(); return workerEventSchema }

// Schema is the same for everything a worker reports back.
func (JobEvent) Schema() avro.Schema { buildEventSchemas(); return jobEventSchema }

func buildEventSchemas() {
	eventSchemaOnce.Do(func() {
		var err error
		if workerEventSchema, err = envelopeSchema("WorkerEvent", distinctBodies(WorkerEventBodies)); err != nil {
			panic(fmt.Sprintf("models: worker event schema: %v", err))
		}
		if jobEventSchema, err = envelopeSchema("JobEvent", distinctBodies(JobEventBodies)); err != nil {
			panic(fmt.Sprintf("models: job event schema: %v", err))
		}
	})
}

// envelopeSchema is the record both envelopes share: the discriminator, and a
// union of every body it can carry.
func envelopeSchema(name string, bodies []any) (avro.Schema, error) {
	// One cache per schema document. Avro names a record once and refers to it
	// by name after that, so the same Go struct reached through two fields has
	// to resolve to the same schema object: oauth2.Token is reachable three
	// ways through a single worker command, and defining it three times is a
	// document the registry rejects outright.
	//
	// Per document rather than global, because a name defined in one envelope's
	// schema means nothing in the other's.
	seen := map[reflect.Type]avro.Schema{}
	branches := make([]avro.Schema, 0, len(bodies))
	for _, body := range bodies {
		t := reflect.TypeOf(body)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		s, err := schemaOf(t, "", seen)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		record, ok := s.(*avro.RecordSchema)
		if !ok {
			return nil, fmt.Errorf("%s: resolved to %s, not a record", t, s.Type())
		}
		// Registered with the pointer-ness the registry declared. A body sent as
		// a value cannot resolve to a branch registered as a pointer, and the
		// handler's type assertion is what decides whether the decoded body is
		// used directly or re-marshalled through JSON.
		avro.Register(record.FullName(), body)
		branches = append(branches, record)
	}
	body, err := avro.NewUnionSchema(branches)
	if err != nil {
		return nil, fmt.Errorf("union: %w", err)
	}
	typeField, err := avro.NewField("type", avro.NewPrimitiveSchema(avro.String, nil))
	if err != nil {
		return nil, err
	}
	bodyField, err := avro.NewField("body", body)
	if err != nil {
		return nil, err
	}
	return avro.NewRecordSchema(name, "warmbly.events", []*avro.Field{typeField, bodyField})
}

var textMarshaler = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()

// schemaOf describes one Go type. `name` disambiguates anonymous records.
func schemaOf(t reflect.Type, name string, seen map[reflect.Type]avro.Schema) (avro.Schema, error) {
	// Pointers are unwrapped before anything else is asked about the type.
	// *time.Time satisfies TextMarshaler exactly as time.Time does, so testing
	// that first sent every optional timestamp as a string and the zero value
	// then failed to parse on the way back.
	if t.Kind() == reflect.Pointer {
		// Optional, so an absent value stays absent instead of decoding as a
		// zero the receiver cannot tell from a real one.
		inner, err := schemaOf(t.Elem(), name, seen)
		if err != nil {
			return nil, err
		}
		return avro.NewUnionSchema([]avro.Schema{&avro.NullSchema{}, inner})
	}
	// A timestamp is a number with a logical type, not text: it is ordered and
	// compared by anything reading the topic.
	if t == reflect.TypeOf(time.Time{}) {
		return avro.NewPrimitiveSchema(avro.Long, avro.NewPrimitiveLogicalSchema(avro.TimestampMillis)), nil
	}
	// A type that writes itself as text travels as a string, which is how
	// uuid.UUID and the enum-ish string types stay readable in the registry.
	if t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler) {
		return avro.NewPrimitiveSchema(avro.String, nil), nil
	}
	switch t.Kind() {
	case reflect.Bool:
		return avro.NewPrimitiveSchema(avro.Boolean, nil), nil
	case reflect.String:
		return avro.NewPrimitiveSchema(avro.String, nil), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return avro.NewPrimitiveSchema(avro.Int, nil), nil
	case reflect.Int64, reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return avro.NewPrimitiveSchema(avro.Long, nil), nil
	case reflect.Float32:
		return avro.NewPrimitiveSchema(avro.Float, nil), nil
	case reflect.Float64:
		return avro.NewPrimitiveSchema(avro.Double, nil), nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return avro.NewPrimitiveSchema(avro.Bytes, nil), nil
		}
		inner, err := schemaOf(t.Elem(), name, seen)
		if err != nil {
			return nil, err
		}
		return avro.NewArraySchema(inner), nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map key %s is not a string", t.Key())
		}
		inner, err := schemaOf(t.Elem(), name, seen)
		if err != nil {
			return nil, err
		}
		return avro.NewMapSchema(inner), nil
	case reflect.Struct:
		return recordSchema(t, name, seen)
	default:
		return nil, fmt.Errorf("unsupported kind %s", t.Kind())
	}
}

func recordSchema(t reflect.Type, name string, seen map[reflect.Type]avro.Schema) (avro.Schema, error) {
	// A second sighting becomes a reference to the first definition. Avro names
	// a record once and refers to it by name after that, and a document that
	// defines the same name twice is rejected outright. Reusing the same schema
	// object is not enough: it is serialised in full wherever it appears.
	if cached, ok := seen[t]; ok {
		named, ok := cached.(avro.NamedSchema)
		if !ok {
			return cached, nil
		}
		return avro.NewRefSchema(named), nil
	}
	fields := make([]*avro.Field, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Unexported fields are not on the wire under any codec: encoding/json
		// cannot see them either. Descending into one is how a dependency's
		// private `any` field became our problem.
		if !f.IsExported() {
			continue
		}
		fieldName := f.Tag.Get("avro")
		// Checked before descending, which is the half upstream gets wrong: a
		// field excluded on purpose must not have to be describable.
		if fieldName == "-" {
			continue
		}
		if fieldName == "" {
			fieldName = f.Name
		}
		s, err := schemaOf(f.Type, t.Name()+"_"+f.Name, seen)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name(), f.Name, err)
		}
		field, err := avro.NewField(fieldName, s)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name(), f.Name, err)
		}
		fields = append(fields, field)
	}
	recordName := t.Name()
	if recordName == "" {
		recordName = name
	}
	record, err := avro.NewRecordSchema(recordName, "warmbly.events", fields)
	if err != nil {
		return nil, err
	}
	seen[t] = record
	return record, nil
}

// distinctBodies reduces a body registry to the distinct types in it, in a
// stable order. Distinct because several event types share a body and a union
// may not list a branch twice; stable because branch order is part of the
// schema, and one that reordered itself between boots would register a new
// version on every deploy.
func distinctBodies[K ~string](registry map[K]any) []any {
	seen := map[reflect.Type]bool{}
	out := make([]any, 0, len(registry))
	for _, body := range registry {
		t := reflect.TypeOf(body)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, body)
	}
	sort.Slice(out, func(i, j int) bool { return elemName(out[i]) < elemName(out[j]) })
	return out
}

func elemName(body any) string {
	t := reflect.TypeOf(body)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}
