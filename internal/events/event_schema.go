package events

import (
	"fmt"

	"github.com/hamba/avro/v2"
	"github.com/warmbly/warmbly/internal/models"
)

// The analytics streams describe themselves for the same reason the bus
// envelopes do: the Avro codec refuses a value that carries no schema rather
// than reflecting over it, and without these both email-events and
// warmup-events failed at serialize on every send. The failure is warn-logged
// by the callers, so the only symptom was two topics that never got a schema
// and never got a record.
//
// TrackingEvent deliberately has none. The Rust service supplies that subject's
// schema under its own name, and a second definition derived here would be a
// different name on the same subject.
var (
	emailSentSchema  = mustSchema(EmailSentEvent{})
	warmupSentSchema = mustSchema(WarmupEmailSentEvent{})
)

// Schema lets the Avro codec frame a campaign send record.
func (EmailSentEvent) Schema() avro.Schema { return emailSentSchema }

// Schema is the same for a warmup send record.
func (WarmupEmailSentEvent) Schema() avro.Schema { return warmupSentSchema }

// mustSchema fails at init rather than at the first send, because these structs
// are fixed and one that cannot be described is a build problem, not a runtime.
func mustSchema(v any) avro.Schema {
	s, err := models.SchemaFor(v)
	if err != nil {
		panic(fmt.Sprintf("events: %T schema: %v", v, err))
	}
	return s
}
