package admindevice

import (
	"encoding/json"
	"testing"
)

func TestAdminDevicePendingResponseDoesNotReturnUserOrToken(t *testing.T) {
	raw, err := json.Marshal(Poll{Status: "pending"})
	if err != nil || string(raw) != `{"status":"pending"}` {
		t.Fatal("pending response differs from contract", string(raw), err)
	}
}
