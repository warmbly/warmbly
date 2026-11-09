package orgtransfer

import (
	"encoding/json"
	"testing"
)

// An archived client secret that is not sealed is a placeholder, never a secret to store.
func TestDropUnsealedOAuthSecret(t *testing.T) {
	for in, want := range map[string]string{`"***"`: ``, `""`: ``} {
		row := map[string]json.RawMessage{"client_secret": json.RawMessage(in)}
		dropUnsealedOAuthSecret(nil, row)
		if got := jsonString(row["client_secret"]); got != want {
			t.Errorf("%s kept as %q", in, got)
		}
	}
}
