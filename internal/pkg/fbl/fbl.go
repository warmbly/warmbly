package fbl

import (
	"regexp"
	"strings"
)

type Report struct {
	Complaint         bool
	OriginalMessageID string
	Recipient         string
	FeedbackType      string
}

var (
	feedbackType = regexp.MustCompile(`(?im)^\s*Feedback-Type:\s*([^\s;]+)`)
	originalRcpt = regexp.MustCompile(`(?im)^\s*(?:Original-Rcpt-To|Original-Recipient):\s*(?:rfc822;\s*)?<?([^\s<>]+@[^\s<>]+)>?`)
	messageID    = regexp.MustCompile(`(?im)^\s*(?:Original-Message-ID|Message-ID):\s*<([^>\s]+)>`)
)

func Detect(contentType, body string) bool {
	lowerType := strings.ToLower(contentType)
	if strings.Contains(lowerType, "report-type=feedback-report") || strings.Contains(lowerType, "message/feedback-report") {
		return true
	}
	return feedbackType.MatchString(body)
}

func Parse(body string) Report {
	var report Report
	if match := feedbackType.FindStringSubmatch(body); len(match) > 1 {
		report.FeedbackType = strings.ToLower(strings.TrimSpace(match[1]))
		report.Complaint = report.FeedbackType == "abuse" || report.FeedbackType == "fraud"
	}
	if match := originalRcpt.FindStringSubmatch(body); len(match) > 1 {
		report.Recipient = strings.TrimSpace(match[1])
	}
	if matches := messageID.FindAllStringSubmatch(body, -1); len(matches) > 0 {
		report.OriginalMessageID = strings.TrimSpace(matches[len(matches)-1][1])
	}
	return report
}
