package orgtransfer

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func assertExportJSONEqual(t *testing.T, actual, expected []byte) {
	t.Helper()
	var a, b any
	for _, item := range []struct {
		raw []byte
		out *any
	}{{actual, &a}, {expected, &b}} {
		decoder := json.NewDecoder(bytes.NewReader(item.raw))
		decoder.UseNumber()
		if err := decoder.Decode(item.out); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("portable evidence changed", a, b)
	}
}
