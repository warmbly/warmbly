package codec

import (
	"context"
	"testing"
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
