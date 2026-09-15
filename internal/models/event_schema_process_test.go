package models

import (
	"encoding/base64"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/hamba/avro/v2"
)

// Production is four processes, and one of them only ever decodes what another
// encoded. Every other test here encodes and decodes in one process, which
// registers the union's Go types as a side effect of building the schema, so
// the decode half was never tested from a cold start.
//
// That gap is why an Avro cutover failed: the worker decoded ADD_EMAIL into a
// body with every field empty, reported "Unsupported email provider" for all 39
// mailboxes, and never logged a decode error. Silence, not a failure.
//
// So the decoder here is a child process that has never encoded anything.
const decodeHelperEnv = "WARMBLY_DECODE_HELPER"

func TestDecodeInAProcessThatHasNeverEncoded(t *testing.T) {
	if payload := os.Getenv(decodeHelperEnv); payload != "" {
		runDecodeHelper(t, payload)
		return
	}

	schema := WorkerEvent{}.Schema()
	in := WorkerEvent{Type: WorkerEventTypeAddEmail, Body: sample(WorkerEventBodies[WorkerEventTypeAddEmail])}
	raw, err := avro.Marshal(schema, in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run", "TestDecodeInAProcessThatHasNeverEncoded", "-test.v")
	cmd.Env = append(os.Environ(), decodeHelperEnv+"="+base64.StdEncoding.EncodeToString(raw))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("decoder process failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "DECODED-OK") {
		t.Fatalf("decoder process did not confirm the body type:\n%s", out)
	}
}

// runDecodeHelper is the child. It must not build a schema before decoding,
// because that is the side effect being tested for.
func runDecodeHelper(t *testing.T, encoded string) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("helper: payload: %v", err)
	}
	// The schema arrives from the registry in production; here it is rebuilt,
	// which is the one thing the child is allowed to do first because a
	// decoder must always obtain the writer's schema somehow.
	schema := WorkerEvent{}.Schema()

	var out WorkerEvent
	if err := avro.Unmarshal(schema, raw, &out); err != nil {
		t.Fatalf("helper: decode: %v", err)
	}
	want := reflect.TypeOf(sample(WorkerEventBodies[WorkerEventTypeAddEmail]))
	if got := reflect.TypeOf(out.Body); got != want {
		t.Fatalf("helper: body decoded as %v, want %v", got, want)
	}
	body, ok := out.Body.(*AddWorkerEmail)
	if !ok {
		t.Fatalf("helper: body is %T", out.Body)
	}
	if body.Type == "" || body.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("helper: body decoded empty: provider=%q id=%s", body.Type, body.ID)
	}
	t.Log("DECODED-OK")
}
