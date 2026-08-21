package generation

import (
	"strings"
	"testing"
)

func TestStripQuotedCutsAtAttribution(t *testing.T) {
	body := "Thanks, that works for me. Thursday at 2pm is good and I will bring the ops lead.\n\n" +
		"On Mon, 3 Feb 2026 at 09:12, Ana <ana@example.com> wrote:\n> Does Thursday work?\n> Ana"
	got := StripQuoted(body)
	if strings.Contains(got, "Does Thursday work?") || strings.Contains(got, "wrote:") {
		t.Fatalf("quoted history survived: %q", got)
	}
	if !strings.HasPrefix(got, "Thanks, that works") {
		t.Fatalf("reply lost: %q", got)
	}
}

func TestStripQuotedKeepsBottomPostedReply(t *testing.T) {
	// Nothing meaningful above the attribution: cutting there would throw the
	// whole reply away.
	body := "Hi\n\nOn Mon, Ana wrote:\n> the question\n\nAnswer: yes, Thursday works."
	got := StripQuoted(body)
	if !strings.Contains(got, "Answer: yes") {
		t.Fatalf("bottom-posted reply lost: %q", got)
	}
	if strings.Contains(got, "> the question") {
		t.Fatalf("quoted line survived: %q", got)
	}
}

func TestStripQuotedDropsSignature(t *testing.T) {
	got := StripQuoted("See you then.\n\n--\nAna Rodríguez\nSunrise Labs")
	if strings.Contains(got, "Sunrise Labs") {
		t.Fatalf("signature survived: %q", got)
	}
}

func TestRenderThreadSpendsBudgetOnTheNewestMessage(t *testing.T) {
	long := strings.Repeat("word ", 400) // 2000 chars
	msgs := []GroundingMessage{
		{From: "a@x.com", Subject: "Hi", Body: long, Preview: "old preview"},
		{From: "b@x.com", Subject: "Re: Hi", Body: "the newest thing they said", Preview: "new preview"},
	}
	out := RenderThread(msgs, 100, 100)
	if !strings.Contains(out, "the newest thing they said") {
		t.Fatalf("newest message not rendered in full: %q", out)
	}
	if !strings.Contains(out, "old preview") {
		t.Fatalf("older message should degrade to its preview: %q", out)
	}
	// Oldest first, so the older message comes before the newer one.
	if strings.Index(out, "old preview") > strings.Index(out, "the newest thing") {
		t.Fatalf("thread rendered newest first: %q", out)
	}
}

func TestRenderThreadFallsBackToPreviewWithoutBody(t *testing.T) {
	out := RenderThread([]GroundingMessage{{From: "a@x.com", Subject: "Hi", Preview: "only a preview"}}, 0, 0)
	if !strings.Contains(out, "only a preview") {
		t.Fatalf("preview fallback missing: %q", out)
	}
}
