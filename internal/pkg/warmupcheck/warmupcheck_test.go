package warmupcheck

import (
	"strings"
	"testing"
)

func TestCheckPlainCopy(t *testing.T) {
	res := Check("Quick question about pricing", "Hi Sam,\nSaw your post on outbound. Are you still comparing tools?\nThanks,\nJo")
	if res.SubjectLength != len("Quick question about pricing") {
		t.Fatalf("subject length = %d", res.SubjectLength)
	}
	if res.WordCount != 18 {
		t.Fatalf("word count = %d, want 18", res.WordCount)
	}
	if res.ReadingTimeSeconds != 5 {
		t.Fatalf("reading time = %d, want 5", res.ReadingTimeSeconds)
	}
	if res.LinkCount != 0 {
		t.Fatalf("link count = %d", res.LinkCount)
	}
	if res.QuestionCount != 1 {
		t.Fatalf("question count = %d, want 1", res.QuestionCount)
	}
	if len(res.SpamTriggerHits) != 0 {
		t.Fatalf("trigger hits = %v", res.SpamTriggerHits)
	}
	if res.BlockedReason != "" {
		t.Fatalf("plain copy blocked: %q", res.BlockedReason)
	}
}

func TestCheckCountsLinksAndTriggers(t *testing.T) {
	res := Check("Free trial inside", "Act now at https://example.com/x?free=true to claim your cash bonus.")
	if res.LinkCount != 1 {
		t.Fatalf("link count = %d, want 1", res.LinkCount)
	}
	found := map[string]bool{}
	for _, h := range res.SpamTriggerHits {
		found[h] = true
	}
	for _, want := range []string{"free", "act now", "cash", "cash bonus"} {
		if !found[want] {
			t.Fatalf("missing trigger hit %q in %v", want, res.SpamTriggerHits)
		}
	}
	if !strings.Contains(res.BlockedReason, "spam-trigger") {
		t.Fatalf("send gate not reported: %q", res.BlockedReason)
	}
	// The slug inside the URL is masked, so it must not double-count "free".
	for _, h := range res.SpamTriggerHits {
		if strings.Contains(h, "example.com") {
			t.Fatalf("URL slug reported as copy: %v", res.SpamTriggerHits)
		}
	}
}

func TestCheckEmpty(t *testing.T) {
	res := Check("", "   ")
	if res.WordCount != 0 || res.ReadingTimeSeconds != 0 || res.SubjectLength != 0 {
		t.Fatalf("empty input produced %+v", res)
	}
}
