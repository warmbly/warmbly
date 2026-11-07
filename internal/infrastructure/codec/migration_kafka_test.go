//go:build kafka

package codec

import (
	"reflect"
	"testing"

	"github.com/iskorotkov/avro/v2"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMigrationCodecReadsPriorCommandsAndResults(t *testing.T) {
	avroCodec := &AvroCodec{client: &fakeRegistry{}, ids: map[string]int{}, schemas: map[int]avro.Schema{}}
	for _, event := range []any{
		models.WorkerEvent{Type: models.WorkerEventTypeSendEmail, Body: &models.SendEmail{Subject: "fixture"}},
		models.JobEvent{Type: models.JobEventTypeEmailSent, Body: models.SendEmailResult{Success: true, MessageID: "accepted@example.test"}},
	} {
		avroCodec.schemas[7] = event.(interface{ Schema() avro.Schema }).Schema()
		for _, writer := range []Codec{avroCodec, NewJSON()} {
			reader := &migrationCodec{writer: NewJSON(), avro: avroCodec, json: NewJSON()}
			payload, err := writer.Serialize(t.Context(), "fixture", event)
			if err != nil {
				t.Fatal(err)
			}
			prior, current := reflect.New(reflect.TypeOf(event)).Interface(), reflect.New(reflect.TypeOf(event)).Interface()
			if err := writer.Deserialize(t.Context(), "fixture", payload, prior); err != nil {
				t.Fatal(err)
			}
			if err := reader.Deserialize(t.Context(), "fixture", payload, current); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(prior, current) {
				t.Fatalf("%s event changed", writer.Name())
			}
		}
	}
}
