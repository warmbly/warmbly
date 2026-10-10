package nodeevidence

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvidenceDropsUnknownFieldsAndCanonicalizesCategory(t *testing.T) {
	var e Event
	if err := json.Unmarshal([]byte(`{"event":"sync_control_plane_held","category":"provider","level":"healthy","http_status":503,"error":"password=secret","body":"private mail","headers":{"Authorization":"Bearer secret"}}`), &e); err != nil {
		t.Fatal(err)
	}
	safe, ok := Sanitize(e)
	if !ok || safe.Category != "control_plane" || safe.Level != "warn" || safe.HTTPStatus != 503 {
		t.Fatal(safe)
	}
	raw, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "private mail") || strings.Contains(string(raw), "Authorization") {
		t.Fatal("sensitive metadata forwarded")
	}
	if _, ok := Sanitize(Event{Name: "password=secret"}); ok {
		t.Fatal("arbitrary event text accepted")
	}
	if safe, _ := Sanitize(Event{Name: ProviderAuth, HTTPStatus: 503, Count: -1}); safe.HTTPStatus != 0 || safe.Count != 0 {
		t.Fatal("invalid fields accepted", safe)
	}
}
