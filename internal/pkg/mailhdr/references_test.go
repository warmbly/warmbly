package mailhdr

import "testing"

func TestReferencesPreserveOrderedAncestryAndRejectHeaderText(t *testing.T) {
	got, err := References([]string{"<root@example.test>", "parent@example.test", "root@example.test", "child@example.test"})
	if err != nil || got != "<root@example.test> <parent@example.test> <child@example.test>" {
		t.Fatalf("ancestry=%q %v", got, err)
	}
	for _, bad := range []string{"missing-at", "id@example.test\r\nBcc:x@y.test", "id @example.test", "<id\x00@example.test>"} {
		if _, err := References([]string{bad}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
