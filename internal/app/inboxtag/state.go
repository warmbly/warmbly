package inboxtag

import (
	"strings"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
)

// State is exactly what the questions need and nothing else.
//
// Not the whole thread. Accuracy falls as state grows with content no question
// asks about, and these threads carry the entire conversation quoted inside
// every reply, so a naive dump would bury the four new lines under forty old
// ones. The documented context limit is 64k tokens for state plus questions;
// staying far under it is not the point, sending only what is asked about is.
type State struct {
	Subject string `json:"subject"`
	// Body is the latest inbound message as plain text, quoted history removed.
	Body string `json:"body"`
	// PreviousMessage is our last outbound in the thread. Without it "yes" and
	// "that works" mean nothing: the question the reply answers is not in the
	// reply.
	PreviousMessage string `json:"previous_message,omitempty"`
	// Campaign names the outreach this belongs to, which is what makes
	// "cold_inbound" separable from "human_reply".
	Campaign string `json:"campaign,omitempty"`
	// Language names the languages the workspace says its mail is written in.
	// It translates nothing; it only tells the reader what to expect.
	Language string `json:"language,omitempty"`
}

// BuildState assembles the state for one inbound message.
//
// StripQuoted comes from replyclassify rather than a second implementation
// here: it is the same job, it already handles the reply markers several mail
// clients and three languages emit, and two copies would drift.
func BuildState(subject, body, previousMessage, campaign string, langs ...string) State {
	return State{
		Subject:         strings.TrimSpace(subject),
		Body:            trimTo(replyclassify.StripQuoted(body, langs...), BodyLimit),
		PreviousMessage: trimTo(replyclassify.StripQuoted(previousMessage, langs...), BodyLimit),
		Campaign:        strings.TrimSpace(campaign),
	}
}

// trimTo cuts to a rune budget, on a word boundary where one is near the end so
// the last thing the model reads is not half a word.
func trimTo(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	cut := string(r[:limit])
	if i := strings.LastIndexAny(cut, " \n\t"); i > limit-200 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}

// HasContent reports whether there is enough text to be worth a call. An empty
// or near-empty body carries no judgment to make, and asking anyway spends a
// request to be told nothing.
func HasContent(s State) bool {
	return len([]rune(strings.TrimSpace(s.Body))) >= 12 ||
		len([]rune(strings.TrimSpace(s.Subject))) >= 3
}
