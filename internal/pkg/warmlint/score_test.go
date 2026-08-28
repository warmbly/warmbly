package warmlint

import (
	"strings"
	"testing"
)

func hasIssue(res ScoreResult, code string) bool {
	for _, i := range res.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

func TestScoreCleanCopyIsUnpenalized(t *testing.T) {
	res := Score(
		"Quick question about your hiring process",
		"<p>Hi Ada, saw you are growing the support team. Worth a chat?</p>",
		"Hi Ada, saw you are growing the support team. Worth a chat?",
	)
	if res.Score != 100 {
		t.Errorf("clean copy scored %d with %+v, want 100", res.Score, res.Issues)
	}
}

func TestScoreImageHeavyBody(t *testing.T) {
	// Almost all image, no readable text: filters cannot read it, and that is
	// treated as evasion rather than as a design choice.
	res := Score("Hello", `<img src="a.png"><img src="b.png">`, "")
	if !hasIssue(res, "image_heavy") {
		t.Errorf("image-only body not flagged: %+v", res.Issues)
	}

	// Plenty of text, a few images: fine.
	body := strings.Repeat("A real sentence about the recipient's work. ", 20)
	res = Score("Hello", "<p>"+body+`</p><img src="a.png">`, body)
	if hasIssue(res, "image_heavy") || hasIssue(res, "many_images") {
		t.Errorf("ordinary copy with one image was flagged: %+v", res.Issues)
	}

	// Many images alongside text is a softer warning.
	res = Score("Hello", "<p>"+body+`</p>`+strings.Repeat(`<img src="x.png">`, 6), body)
	if !hasIssue(res, "many_images") {
		t.Errorf("six images not flagged: %+v", res.Issues)
	}
	if hasIssue(res, "image_heavy") {
		t.Errorf("text-rich body wrongly flagged as image-only: %+v", res.Issues)
	}
}

func TestScoreWithAttachments(t *testing.T) {
	subject, html, plain := "Following up", "<p>Hi Ada, following up on my note.</p>", "Hi Ada, following up on my note."

	base := ScoreWithAttachments(subject, html, plain, 0)
	if hasIssue(base, "has_attachments") {
		t.Errorf("no attachments should not flag: %+v", base.Issues)
	}

	withAtt := ScoreWithAttachments(subject, html, plain, 2)
	if !hasIssue(withAtt, "has_attachments") {
		t.Errorf("attachments not flagged: %+v", withAtt.Issues)
	}
	if withAtt.Score >= base.Score {
		t.Errorf("attachment score %d not below the clean %d", withAtt.Score, base.Score)
	}
}

func TestScoreNeverGoesNegative(t *testing.T) {
	// Every heuristic at once: the floor is 0, not a negative number the UI
	// would have to special-case.
	res := ScoreWithAttachments(
		"FREE CASH PRIZE GUARANTEED!!!",
		`<img src="a.png"><img src="b.png"><img src="c.png"><img src="d.png">`,
		"",
		5,
	)
	if res.Score < 0 {
		t.Errorf("score = %d, want a floor of 0", res.Score)
	}
	if len(res.Issues) == 0 {
		t.Error("obviously spammy copy produced no issues")
	}
}
