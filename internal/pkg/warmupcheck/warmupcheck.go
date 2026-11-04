// Package warmupcheck statically analyzes an organization-supplied warmup
// template (subject + body) before it enters warmup rotation. It reports what
// the copy looks like, not a deliverability promise.
package warmupcheck

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

var linkPattern = regexp.MustCompile(`https?://[^\s"'<>)\]]*`)

// readingWordsPerMinute is the plain-prose pace used for reading time.
const readingWordsPerMinute = 200

// Result is the static analysis of one template.
type Result struct {
	SubjectLength      int      `json:"subject_length"`
	WordCount          int      `json:"word_count"`
	ReadingTimeSeconds int      `json:"reading_time_seconds"`
	LinkCount          int      `json:"link_count"`
	QuestionCount      int      `json:"question_count"`
	SpamTriggerHits    []string `json:"spam_trigger_hits"`
	// BlockedReason is set when the warmup send gate would refuse this copy.
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// Check analyzes subject and body verbatim; templates are plain text.
func Check(subject, body string) Result {
	combined := strings.TrimSpace(subject + "\n" + body)
	words := len(strings.Fields(combined))
	reading := 0
	if words > 0 {
		reading = (words*60 + readingWordsPerMinute/2) / readingWordsPerMinute
		if reading < 1 {
			reading = 1
		}
	}
	return Result{
		SubjectLength:      utf8.RuneCountInString(strings.TrimSpace(subject)),
		WordCount:          words,
		ReadingTimeSeconds: reading,
		LinkCount:          len(linkPattern.FindAllString(combined, -1)),
		QuestionCount:      strings.Count(combined, "?"),
		SpamTriggerHits:    warmlint.TriggerTerms(combined),
		BlockedReason:      blockedReason(subject, body),
	}
}

func blockedReason(subject, body string) string {
	if err := warmlint.Check(subject, body, false); err != nil {
		return err.Error()
	}
	return ""
}
