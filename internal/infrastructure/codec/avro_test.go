//go:build kafka

package codec

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/rest"
	"github.com/iskorotkov/avro/v2"
	"github.com/warmbly/warmbly/internal/models"
)

// Full round-trip coverage for AvroCodec requires a live Confluent Schema
// Registry, which is out of scope for unit tests in this package. The
// integration path is exercised end-to-end by cmd/worker / cmd/backend /
// cmd/consumer against a real registry. These tests cover the parts that can
// be verified in isolation: interface conformance, naming, and the
// nil-serializer guard added by the wrapper.

func TestAvroCodec_Name(t *testing.T) {
	c := &AvroCodec{}
	if got := c.Name(); got != "avro" {
		t.Fatalf("expected name 'avro', got %q", got)
	}
}

func TestAvroCodec_RejectsMalformedCollectionCounts(t *testing.T) {
	for _, schemaJSON := range []string{
		`{"type":"array","items":"long"}`,
		`{"type":"map","values":"long"}`,
	} {
		for _, count := range []int64{65537, math.MaxInt64, math.MinInt64} {
			t.Run(schemaJSON+"/"+strconv.FormatInt(count, 10), func(t *testing.T) {
				schema := avro.MustParse(schemaJSON)
				c := &AvroCodec{client: &fakeRegistry{}, schemas: map[int]avro.Schema{7: schema}}
				payload := binary.AppendVarint([]byte{0, 0, 0, 0, 7}, count)
				var target any = &[]int64{}
				if schema.Type() == avro.Map {
					target = &map[string]int64{}
				}
				if err := c.Deserialize(context.Background(), "topic", payload, target); err == nil {
					t.Fatal("malformed block count was accepted")
				}
			})
		}
	}
}

func TestAvroCodec_CollectionLimitSpansBlocks(t *testing.T) {
	for _, schemaJSON := range []string{
		`{"type":"array","items":"long"}`,
		`{"type":"map","values":"long"}`,
	} {
		t.Run(schemaJSON, func(t *testing.T) {
			schema := avro.MustParse(schemaJSON)
			c := &AvroCodec{client: &fakeRegistry{}, schemas: map[int]avro.Schema{7: schema}}
			payload := binary.AppendVarint([]byte{0, 0, 0, 0, 7}, 65536)
			for i := range 65536 {
				if schema.Type() == avro.Map {
					key := strconv.Itoa(i)
					payload = binary.AppendVarint(payload, int64(len(key)))
					payload = append(payload, key...)
				}
				payload = append(payload, 0)
			}
			var target any = &[]int64{}
			if schema.Type() == avro.Map {
				target = &map[string]int64{}
			}
			if err := c.Deserialize(context.Background(), "topic", append(slices.Clone(payload), 0), target); err != nil {
				t.Fatalf("valid boundary block rejected: %v", err)
			}
			payload = binary.AppendVarint(payload, 1)
			limit := "MaxSliceAllocSize"
			if schema.Type() == avro.Map {
				payload = append(payload, 10, 'e', 'x', 't', 'r', 'a')
				limit = "MaxMapAllocSize"
			}
			payload = append(payload, 0, 0)
			if err := c.Deserialize(context.Background(), "topic", payload, target); err == nil || !strings.Contains(err.Error(), "size is greater than `Config."+limit+"`") {
				t.Fatalf("expected collection limit error across blocks, got %v", err)
			}
		})
	}
}

func TestAvroCodec_ExistingWireFormat(t *testing.T) {
	schema := avro.MustParse(`{"type":"array","items":"long"}`)
	c := &AvroCodec{client: &fakeRegistry{}, schemas: map[int]avro.Schema{7: schema}}
	payload := []byte{0, 0, 0, 0, 7, 6, 2, 4, 6, 0}
	var got []int64
	if err := c.Deserialize(context.Background(), "topic", payload, &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{1, 2, 3}) {
		t.Fatalf("existing wire payload changed: %v", got)
	}
	encoded, err := models.EventAvro.Marshal(schema, got)
	if err != nil || !slices.Equal(encoded, []byte{5, 6, 2, 4, 6, 0}) {
		t.Fatalf("invalid block-size wire encoding: %x, %v", encoded, err)
	}
}

func TestAvroCodec_SerializeWithoutClient(t *testing.T) {
	c := &AvroCodec{}
	if _, err := c.Serialize(context.Background(), "topic", struct{}{}); err == nil {
		t.Fatal("expected error when serializer is not configured")
	}
}

func TestAvroCodec_DeserializeWithoutClient(t *testing.T) {
	c := &AvroCodec{}
	var target struct{}
	if err := c.Deserialize(context.Background(), "topic", []byte{0x01}, &target); err == nil {
		t.Fatal("expected error when deserializer is not configured")
	}
}

func TestAvroCodec_NilReceiverIsSafe(t *testing.T) {
	var c *AvroCodec
	if _, err := c.Serialize(context.Background(), "topic", struct{}{}); err == nil {
		t.Fatal("expected error on nil receiver")
	}
	var target struct{}
	if err := c.Deserialize(context.Background(), "topic", nil, &target); err == nil {
		t.Fatal("expected error on nil receiver")
	}
}

// fakeRegistry refuses a new schema while the subject's effective level is
// in the forward family, the way a registry does for a new union branch.
type fakeRegistry struct {
	schemaregistry.Client
	global     schemaregistry.Compatibility
	subject    schemaregistry.Compatibility
	subjects   []string
	registered []string
}

func (f *fakeRegistry) GetAllSubjects() ([]string, error) { return f.subjects, nil }

func (f *fakeRegistry) effective() schemaregistry.Compatibility {
	if f.subject != 0 {
		return f.subject
	}
	return f.global
}

func (f *fakeRegistry) Register(subject string, _ schemaregistry.SchemaInfo, _ bool) (int, error) {
	f.registered = append(f.registered, subject)
	switch f.effective() {
	case schemaregistry.Forward, schemaregistry.Full:
		return 0, &rest.Error{Code: 409, Message: "incompatible"}
	}
	return 7, nil
}

func (f *fakeRegistry) GetCompatibility(string) (schemaregistry.Compatibility, error) {
	if f.subject == 0 {
		return 0, &rest.Error{Code: 40408, Message: "no subject-level compatibility"}
	}
	return f.subject, nil
}

func (f *fakeRegistry) GetDefaultCompatibility() (schemaregistry.Compatibility, error) {
	return f.global, nil
}

func (f *fakeRegistry) UpdateCompatibility(_ string, c schemaregistry.Compatibility) (schemaregistry.Compatibility, error) {
	f.subject = c
	return c, nil
}

func TestAvroCodec_RegisterPinsForwardSubjectBackward(t *testing.T) {
	for _, global := range []schemaregistry.Compatibility{schemaregistry.Forward, schemaregistry.Full} {
		reg := &fakeRegistry{global: global}
		c := &AvroCodec{client: reg, ids: map[string]int{}, schemas: map[int]avro.Schema{}}
		ev := models.JobEvent{Type: models.JobEventTypeEmailSent, Body: models.SendEmailResult{}}
		if _, err := c.Serialize(context.Background(), "jobs.worker-events", ev); err != nil {
			t.Fatalf("global %s: %v", global.String(), err)
		}
		if reg.subject != schemaregistry.Backward {
			t.Fatalf("global %s: subject left at %v", global.String(), reg.subject)
		}
	}
}

func TestAvroCodec_RegisterLeavesOtherRefusalsAlone(t *testing.T) {
	reg := &fakeRegistry{global: schemaregistry.Forward, subject: schemaregistry.BackwardTransitive}
	c := &AvroCodec{client: reg, ids: map[string]int{}, schemas: map[int]avro.Schema{}}
	if !isIncompatible(&rest.Error{Code: 409}) || isIncompatible(errors.New("x")) {
		t.Fatal("isIncompatible misreads the refusal")
	}
	if c.pinBackward("s") || reg.subject != schemaregistry.BackwardTransitive {
		t.Fatal("a level that admits a new branch was changed")
	}
}

func TestAvroCodec_RegisterSchemasExpandsPerNodeTopics(t *testing.T) {
	reg := &fakeRegistry{global: schemaregistry.Backward, subjects: []string{
		"w.a-value", "w.b-value", "warmup-events-value", "jobs.worker-events-value",
	}}
	c := &AvroCodec{client: reg, ids: map[string]int{}, schemas: map[int]avro.Schema{}}
	err := c.RegisterSchemas(context.Background(), map[string]avro.Schema{
		"w.*":                models.WorkerEvent{}.Schema(),
		"jobs.worker-events": models.JobEvent{}.Schema(),
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(reg.registered)
	want := []string{"jobs.worker-events-value", "w.a-value", "w.b-value"}
	if !slices.Equal(reg.registered, want) {
		t.Fatalf("registered %v, want %v", reg.registered, want)
	}
}
