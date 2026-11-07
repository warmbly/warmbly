package templates

import (
	"fmt"
	"strings"
	"testing"
)

func TestDigestListsTheNewestAndCountsTheRest(t *testing.T) {
	items := make([]DigestItem, 12)
	for i := range items {
		items[i] = DigestItem{Title: fmt.Sprintf("Item-%02d", i)}
	}
	html, err := GenerateDigestHTML(len(items), items)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range items {
		if shown := strings.Contains(html, item.Title); shown != (i < DigestShown) {
			t.Fatalf("%s shown=%v", item.Title, shown)
		}
	}
	if !strings.Contains(html, "And 3 more waiting") || !strings.Contains(html, "You have 12 notifications waiting") {
		t.Fatal("digest does not count the notifications it leaves out")
	}
	if got := DigestSubject(12); !strings.HasPrefix(got, "12 notifications waiting in ") {
		t.Fatalf("subject %q", got)
	}
}
