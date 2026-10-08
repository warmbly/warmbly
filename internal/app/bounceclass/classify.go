// Package bounceclass names what a bounce is about when the reason text does
// not say so, so a rejection aimed at the sending mailbox is not held against
// the recipient. Only reasons that do not name the recipient reach it.
package bounceclass

import (
	"context"
	"fmt"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// Cause is the mutually exclusive answer: what the receiving server refused.
// These identifiers are stored in event metadata, so changing one changes
// history.
const (
	CauseRecipientInvalid = "recipient_invalid"
	CauseMailboxFull      = "mailbox_full"
	CauseReputationBlock  = "reputation_block"
	CauseTransient        = "transient"
	CauseOther            = "other"
)

var causeCriteria = map[string]string{
	CauseRecipientInvalid: "The address does not exist, is disabled, or cannot receive mail",
	CauseMailboxFull:      "The mailbox is full or over quota",
	CauseReputationBlock:  "The receiving server refused the sending server, domain, or message for reputation, policy, blocklist, or spam reasons",
	CauseTransient:        "A temporary failure: greylisting, server busy, try again later",
	CauseOther:            "None of the above",
}

// questionCause is the one question's key in the request and the response.
const questionCause = "cause"

// ConfFloor is the confidence below which a verdict changes nothing. A
// suppression is silent and permanent, so the bar is the strong one, the same
// number the inbox tagger requires before it may suppress an address.
const ConfFloor = 0.80

// ReasonLimit caps the reason sent as state, in runes. DSN prose is short;
// the cap is for the report that pasted a whole transcript.
const ReasonLimit = 1500

// Verdict is one classified bounce reason.
type Verdict struct {
	Cause      string
	Confidence float64
	Model      string
}

// AddressIsFine reports whether the bounce is about something other than the
// recipient's address, strongly enough to skip suppressing it. A nil verdict
// says nothing, so the address is not vouched for.
func (v *Verdict) AddressIsFine() bool {
	if v == nil || v.Confidence < ConfFloor {
		return false
	}
	switch v.Cause {
	case CauseReputationBlock, CauseTransient, CauseMailboxFull:
		return true
	}
	return false
}

// Classify asks one question about one bounce reason. An empty reason has
// nothing to classify and returns nil, nil.
func Classify(ctx context.Context, asker typesafe.Asker, reason string) (*Verdict, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, nil
	}
	if asker == nil {
		return nil, fmt.Errorf("bounceclass: no asker configured")
	}
	if r := []rune(reason); len(r) > ReasonLimit {
		reason = string(r[:ReasonLimit])
	}

	state := map[string]string{"reason": reason}
	questions := map[string]typesafe.Question{
		questionCause: typesafe.Choice("Why did the receiving server refuse this email?", causeCriteria),
	}
	resp, err := asker.Ask(typesafe.WithUsage(ctx, "bounce_classification", ""), state, questions)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("bounceclass: empty response")
	}
	ans, ok := resp.Answers[questionCause]
	if !ok {
		return nil, fmt.Errorf("bounceclass: response carries no %q answer", questionCause)
	}
	if _, known := causeCriteria[ans.Choice]; !known {
		return nil, fmt.Errorf("bounceclass: unknown cause %q", ans.Choice)
	}
	return &Verdict{Cause: ans.Choice, Confidence: ans.Confidence, Model: resp.Model}, nil
}
