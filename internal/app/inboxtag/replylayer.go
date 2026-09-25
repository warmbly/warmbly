package inboxtag

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
	"github.com/warmbly/warmbly/internal/repository"
)

// The reply classifier's model layer, backed by TypeSafe: a stored tagging
// verdict when the message has one, otherwise one three-way choice question
// with a real confidence.

// ReplyClassFor maps a tagging verdict onto the reply classifier's classes,
// which are the contract campaign branching and stop_on_reply read.
func ReplyClassFor(kind, intent string) string {
	switch kind {
	case KindAutoReplyOOO:
		return replyclassify.ClassOutOfOffice
	case KindAutoReplyTicket:
		return replyclassify.ClassAutoReply
	case KindHumanReply:
	default:
		return replyclassify.ClassUnknown
	}
	switch intent {
	case IntentAgreed, IntentWantsInfo, IntentWantsPricing, IntentScheduling,
		IntentInProgress, IntentQuestionAnswered:
		return replyclassify.ClassPositive
	case IntentNotInterested, IntentWrongPerson:
		return replyclassify.ClassNegative
	case IntentOptOut:
		return replyclassify.ClassUnsubscribe
	case IntentNotNow, IntentUnclear:
		return replyclassify.ClassNeutral
	}
	return replyclassify.ClassUnknown
}

// sentimentCriteria is the three-way question the model layer answers when no
// stored verdict exists. Same classes, same floor, as the full taxonomy.
var sentimentCriteria = map[string]string{
	replyclassify.ClassPositive: "Interested, agrees, asks for more, or wants to talk",
	replyclassify.ClassNegative: "Declines or says they are not the right person",
	replyclassify.ClassNeutral:  "A question, a deferral, or anything unclear",
}

// ReplyClassifier builds the classifier's model layer. repo is optional: with
// it, a message the tagger classified moments earlier is answered from the row.
func ReplyClassifier(asker typesafe.Asker, repo repository.InboxTagRepository) replyclassify.TypedClassifyFunc {
	return func(ctx context.Context, in replyclassify.Input) (replyclassify.Result, bool) {
		if repo != nil && in.OrganizationID != uuid.Nil && in.MessageID != "" {
			if stored, err := repo.GetByMessageID(ctx, in.OrganizationID, in.MessageID); err == nil && stored != nil {
				// A verdict the tagger marked untrusted stays untrusted; a
				// coarser second question must not overrule the review page.
				return resultFromStored(stored)
			}
		}
		if asker == nil {
			return replyclassify.Result{}, false
		}
		state := BuildState(in.Subject, in.BodyText, "", "", in.Languages...)
		if !HasContent(state) {
			return replyclassify.Result{}, false
		}
		resp, err := asker.Ask(ctx, state, map[string]Question{
			"sentiment": typesafe.Choice("How does this reply to a cold sales email read?", sentimentCriteria),
		})
		if err != nil {
			return replyclassify.Result{}, false
		}
		a, ok := resp.Answers["sentiment"]
		if !ok || a.Confidence < ConfFloor {
			return replyclassify.Result{}, false
		}
		switch a.Choice {
		case replyclassify.ClassPositive, replyclassify.ClassNegative, replyclassify.ClassNeutral:
			return replyclassify.Result{Class: a.Choice, Confidence: a.Confidence, Source: replyclassify.SourceModel}, true
		}
		return replyclassify.Result{}, false
	}
}

// resultFromStored reads a stored verdict as a classifier result. Only a
// trusted one counts: a kind under the floor, or a human reply whose intent
// was, is not a verdict to route a campaign on.
func resultFromStored(r *repository.InboxTagResult) (replyclassify.Result, bool) {
	if r.KindConfidence < ConfFloor || r.NeedsReview {
		return replyclassify.Result{}, false
	}
	class := ReplyClassFor(r.Kind, r.Intent)
	if class == replyclassify.ClassUnknown {
		return replyclassify.Result{}, false
	}
	conf := r.KindConfidence
	if r.Kind == KindHumanReply {
		conf = r.IntentConfidence
	}
	return replyclassify.Result{Class: class, Confidence: conf, Source: replyclassify.SourceModel}, true
}
