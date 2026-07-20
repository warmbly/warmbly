//go:build !kafka

// Default build: the Avro/Schema-Registry codec is not compiled in (it needs
// confluent-kafka-go / CGO). CODEC_PROVIDER=avro here returns a clear error
// pointing at the `kafka` build tag. Self-host runs on CODEC_PROVIDER=json.
package codec

import "errors"

// ErrAvroNotCompiled is returned when the Avro codec is selected in a build that
// did not include the `kafka` tag.
var ErrAvroNotCompiled = errors.New("codec: avro backend not compiled in; rebuild with -tags kafka (or use CODEC_PROVIDER=json)")

// NewAvro is the stub used in the default (CGO-free) build. The real
// implementation lives in avro.go behind the `kafka` build tag.
func NewAvro(_, _, _ string) (Codec, error) {
	return nil, ErrAvroNotCompiled
}
