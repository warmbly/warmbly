package generation

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Grounding budgets, in characters. A thread used to be grounded on preview
// snippets (a couple of hundred characters each), which is why drafts answered
// the first line of an email and ignored the rest of it. These are the ceiling
// for the other direction: enough of each message to reply to it properly,
// bounded so a long thread cannot turn one draft into a huge prompt.
const (
	GroundingPerMessageChars = 2000
	GroundingTotalChars      = 12000
)

// GroundingMessage is one message of a thread as a model should read it. Body
// is the stored message text, which may be empty for mail synced before bodies
// were indexed; Preview is the one-line snippet that always exists.
type GroundingMessage struct {
	From    string
	Subject string
	Body    string
	Preview string
}

// RenderThread renders a conversation for grounding, oldest first.
//
// The budget is spent newest-first, because the message being replied to
// matters most and the oldest ones are usually pleasantries: once it runs out,
// earlier messages degrade to their preview line rather than dropping out of
// the prompt entirely.
func RenderThread(msgs []GroundingMessage, perMessage, total int) string {
	if perMessage <= 0 {
		perMessage = GroundingPerMessageChars
	}
	if total <= 0 {
		total = GroundingTotalChars
	}

	rendered := make([]string, len(msgs))
	remaining := total
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		text := StripQuoted(m.Body)
		if strings.TrimSpace(text) == "" {
			text = strings.TrimSpace(m.Preview)
		}
		// Once too little budget is left to carry a useful amount of the
		// message, older ones degrade to their preview line instead of a
		// meaningless first sentence.
		floor := min(200, perMessage)
		budget := min(perMessage, remaining)
		if remaining < floor {
			text = strings.TrimSpace(m.Preview)
			budget = floor
		}
		text = truncateRunes(text, budget)
		remaining -= utf8.RuneCountInString(text)
		rendered[i] = text
	}

	var b strings.Builder
	for i, m := range msgs {
		fmt.Fprintf(&b, "From: %s\nSubject: %s\n%s\n\n", m.From, m.Subject, rendered[i])
	}
	return strings.TrimSpace(b.String())
}

// attribution matches the line a mail client writes above quoted history.
var attribution = regexp.MustCompile(`(?im)^\s*(On .{0,120}\bwrote:|-{2,}\s*Original Message\s*-{2,}|_{5,}|From:\s.+\bSent:\s)`)

// StripQuoted removes quoted history and the signature from a message body.
//
// The earlier messages of a thread are already in the prompt on their own, so
// quoting them again spends the budget twice and pushes the actual new content
// out of it.
func StripQuoted(body string) string {
	if body == "" {
		return ""
	}
	text := strings.ReplaceAll(body, "\r\n", "\n")

	// Cut at the attribution line, but only when there is a real message above
	// it: replying underneath the quote is unusual, and losing that reply
	// entirely would be worse than carrying some quoted text.
	if loc := attribution.FindStringIndex(text); loc != nil {
		if head := strings.TrimSpace(text[:loc[0]]); utf8.RuneCountInString(head) > 40 {
			text = head
		}
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		if trimmed == "--" {
			break // RFC 3676 signature delimiter
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max])) + "…"
}
