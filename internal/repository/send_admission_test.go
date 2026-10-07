package repository

import "testing"

func TestMatchOutboundRecipientsNormalizesWithoutBroadening(t *testing.T) {
	if !MatchOutboundRecipients([]string{"Alice <A@example.test>", "b@example.test"}, []string{"B@example.test", "a@example.test"}) {
		t.Fatal("equivalent recipient envelope rejected")
	}
	for _, actual := range [][]string{{"a@example.test"}, {"a@example.test", "b@example.test", "c@example.test"}, {"a@example.test\r\nBcc: hidden@example.test"}, {"not an address"}} {
		if MatchOutboundRecipients([]string{"a@example.test", "b@example.test"}, actual) {
			t.Fatal("broadened or malformed recipient envelope accepted", actual)
		}
	}
}
