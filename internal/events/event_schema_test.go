package events

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hamba/avro/v2"
)

// Every value published through the codec has to describe its own schema; the
// codec refuses one that does not, and the publish sites only warn, so a type
// that loses this is a topic that silently stops carrying records.
func TestAnalyticsEventsCarryASchema(t *testing.T) {
	for _, v := range []any{EmailSentEvent{}, WarmupEmailSentEvent{}} {
		if _, ok := v.(interface{ Schema() avro.Schema }); !ok {
			t.Fatalf("%T does not carry an Avro schema", v)
		}
	}
}

func TestEmailSentEventRoundTrips(t *testing.T) {
	in := EmailSentEvent{
		EventType:  EventTypeEmailSent,
		TaskID:     uuid.New(),
		AccountID:  uuid.New(),
		CampaignID: uuid.New(),
		ContactID:  uuid.New(),
		SequenceID: uuid.New(),
		MessageID:  "<a@warmbly.com>",
		Recipient:  "someone@example.com",
		Subject:    "hello",
		SentAt:     time.Now().Truncate(time.Millisecond).UTC(),
	}
	b, err := avro.Marshal(in.Schema(), in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out EmailSentEvent
	if err := avro.Unmarshal(in.Schema(), b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round trip changed the record:\n got %+v\nwant %+v", out, in)
	}
}

func TestWarmupEmailSentEventRoundTrips(t *testing.T) {
	in := WarmupEmailSentEvent{
		EventType:       EventTypeWarmupEmailSent,
		TaskID:          uuid.New(),
		SenderAccountID: uuid.New(),
		TargetAccountID: uuid.New(),
		MessageID:       "<b@warmbly.com>",
		IsReply:         true,
		SentAt:          time.Now().Truncate(time.Millisecond).UTC(),
	}
	b, err := avro.Marshal(in.Schema(), in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out WarmupEmailSentEvent
	if err := avro.Unmarshal(in.Schema(), b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round trip changed the record:\n got %+v\nwant %+v", out, in)
	}
}
