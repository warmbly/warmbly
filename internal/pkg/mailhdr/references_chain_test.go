package mailhdr

import (
	"fmt"
	"testing"
)

func TestConversationReferencesKeepsTheWholeChainOldestFirst(t *testing.T) {
	root, second, parent := "root.1@mail.test", "second.2@mail.test", "parent.3@mail.test"
	got := ConversationReferences([]string{"<" + root + ">", second, "bad id", root, "<" + parent + ">"})
	want := []string{root, second, parent}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("chain=%v, want %v", got, want)
	}
	if ConversationReferences(nil) != nil || ConversationReferences([]string{"nope"}) != nil {
		t.Fatal("an empty or invalid conversation must produce no chain")
	}
}

// A long conversation still has to fit the header References accepts, so it
// keeps the root and the newest messages and drops the middle.
func TestConversationReferencesTrimsTheMiddleOfALongChain(t *testing.T) {
	var ids []string
	for i := range 40 {
		ids = append(ids, fmt.Sprintf("%02d.0123456789abcdef0123456789abcdef%smail.test", i, "@"))
	}
	got := ConversationReferences(ids)
	if got[0] != ids[0] || got[len(got)-1] != ids[len(ids)-1] {
		t.Fatalf("chain lost its root or its parent: first=%s last=%s", got[0], got[len(got)-1])
	}
	if len(got) >= len(ids) {
		t.Fatalf("chain of %d was not trimmed", len(got))
	}
	for i := 2; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("tail out of order at %d: %v", i, got)
		}
	}
	if _, err := References(got); err != nil {
		t.Fatalf("trimmed chain still refused: %v", err)
	}
}
