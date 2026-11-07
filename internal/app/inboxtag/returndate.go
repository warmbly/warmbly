package inboxtag

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/repository"
)

// The out-of-office hold resumes a lead on the return date the rule-based
// parser reads. When the workspace tags its inbox, the model is asked whether
// the phrase that date came from really says when the sender is back, and a
// firm no sends the hold to the workspace's fallback instead.

// returnDateToConfirm is the parsed date worth asking about: a live message
// that is or may be an away message, in a workspace whose hold reads the
// answer, with a date the parser found.
func returnDateToConfirm(m Message, ws workspaceSettings, deterministicKind string, now time.Time) (replyclassify.ReturnDate, bool) {
	if m.historical || !ws.holdsOnOOO {
		return replyclassify.ReturnDate{}, false
	}
	if deterministicKind != "" && deterministicKind != KindAutoReplyOOO {
		return replyclassify.ReturnDate{}, false
	}
	return replyclassify.FindReturnDate(m.Subject, m.holdBody(), now, ws.languages...)
}

// holdBody is the text the out-of-office hold reads its date from: the body,
// else the snippet, as holdForOutOfOffice does.
func (m Message) holdBody() string {
	if strings.TrimSpace(m.BodyText) != "" {
		return m.BodyText
	}
	return m.Snippet
}

// ReturnDateDoubted reports a stored verdict whose model read the phrase behind
// back as not saying when the sender returns. Only an answer about that same
// day counts: a verdict that was not asked, or was asked about another date,
// leaves the parser's date standing.
func ReturnDateDoubted(r *repository.InboxTagResult, back time.Time) bool {
	if r == nil || r.ReturnDate == nil || r.Kind != KindAutoReplyOOO {
		return false
	}
	if r.ReturnDate.UTC().Format(time.DateOnly) != back.UTC().Format(time.DateOnly) {
		return false
	}
	var answers map[string]Answer
	if err := json.Unmarshal(r.Answers, &answers); err != nil {
		return false
	}
	a, ok := answers[QReturnDate]
	return ok && a.Noul < ReturnDateFloor
}
