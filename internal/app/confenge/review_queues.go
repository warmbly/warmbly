package confenge

import "strings"

// ReviewItem is one cockpit row. Unready items never share the READY queue.
type ReviewItem struct {
	Queue               string `json:"queue"`
	Company             string `json:"company"`
	Person              string `json:"person,omitempty"`
	Role                string `json:"role,omitempty"`
	Service             string `json:"service"`
	MentionableEvidence string `json:"mentionable_evidence,omitempty"`
	WhyNow              string `json:"why_now,omitempty"`
	Body                string `json:"body,omitempty"`
	Sources             string `json:"sources,omitempty"`
	Confidence          string `json:"confidence,omitempty"`
	Risk                string `json:"risk,omitempty"`
	RecipientState      string `json:"recipient_state"`
	MessageabilityState string `json:"messageability_state"`
	NextAction          string `json:"next_action,omitempty"`
}

// ClassifyReviewQueue routes an audit row. READY review requires sendable body.
func ClassifyReviewQueue(row CorpusAuditRow) string {
	if row.FinalState == MessageabilityReady && strings.TrimSpace(row.SendableBody) != "" && row.RecipientState == RecipientValidated {
		return ReviewQueueReady
	}
	if row.RecipientState == RecipientException || row.Queue == ReviewQueueException {
		return ReviewQueueException
	}
	if row.FinalState == MessageabilityBlocked || row.RecipientState == RecipientBlocked {
		return ReviewQueueBlocked
	}
	return ReviewQueueEnrichment
}

// SplitReviewQueues partitions rows so Tiago never opens junk in the READY list.
func SplitReviewQueues(rows []CorpusAuditRow) (ready, exception, enrichment, blocked []ReviewItem) {
	for _, r := range rows {
		item := ReviewItem{
			Queue:               ClassifyReviewQueue(r),
			Company:             r.Company,
			Person:              "",
			Service:             r.Service,
			MentionableEvidence: r.MentionableEvidence,
			WhyNow:              r.WhyNow,
			Body:                r.SendableBody,
			RecipientState:      r.RecipientState,
			MessageabilityState: r.MessageabilityState,
			NextAction:          r.NextAction,
		}
		if r.RecipientState == RecipientValidated {
			// Person/role only when validated (already proven).
		}
		switch item.Queue {
		case ReviewQueueReady:
			ready = append(ready, item)
		case ReviewQueueException:
			item.Body = ""
			exception = append(exception, item)
		case ReviewQueueBlocked:
			item.Body = ""
			blocked = append(blocked, item)
		default:
			item.Body = ""
			enrichment = append(enrichment, item)
		}
	}
	return ready, exception, enrichment, blocked
}
