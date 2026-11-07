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
	replyclassify.ClassPositive: "Interested, agrees, wants to talk, or asks about price, terms or how it works",
	replyclassify.ClassNegative: "Declines or says they are not the right person",
	replyclassify.ClassNeutral:  "Says the timing is wrong, or anything unclear",
}

// ReplyClassifier builds the classifier's model layer. repo is optional: with
// it, a message the tagger classified moments earlier is answered from the row,
// which costs nothing and so is read whatever mayAsk says.
func ReplyClassifier(asker typesafe.Asker, repo repository.InboxTagRepository) replyclassify.TypedClassifyFunc {
	return func(ctx context.Context, in replyclassify.Input, mayAsk func() bool) (replyclassify.Result, bool) {
		if r, ok := StoredReplyResult(ctx, repo, in.OrganizationID, in.MessageID); ok {
			return r, true
		}
		if asker == nil || !mayAsk() {
			return replyclassify.Result{}, false
		}
		state := BuildState(in.Subject, in.BodyText, "", "", in.Languages...)
		if !HasContent(state) {
			return replyclassify.Result{}, false
		}
		resp, err := asker.Ask(typesafe.WithUsage(ctx, "reply_classification", in.OrganizationID.String()), state, map[string]Question{
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

// StoredReplyResult reads the verdict the tagger stored for a message as a
// reply class. found is false only when nothing was stored; an untrusted
// verdict is found as ClassUnknown, so no other layer labels what the inbox
// shows as Needs review.
func StoredReplyResult(ctx context.Context, repo repository.InboxTagRepository, orgID uuid.UUID, messageID string) (replyclassify.Result, bool) {
	if repo == nil || orgID == uuid.Nil || messageID == "" {
		return replyclassify.Result{}, false
	}
	stored, err := repo.GetByMessageID(ctx, orgID, messageID)
	if err != nil || stored == nil {
		return replyclassify.Result{}, false
	}
	return resultFromStored(stored), true
}

// resultFromStored reads a stored verdict as a classifier result. Only a
// trusted one names a class: a kind under the floor, or a human reply whose
// intent was, is not a verdict to route a campaign on.
func resultFromStored(r *repository.InboxTagResult) replyclassify.Result {
	unknown := replyclassify.Result{Class: replyclassify.ClassUnknown, Source: replyclassify.SourceModel}
	if r.KindConfidence < ConfFloor || r.NeedsReview {
		return unknown
	}
	class := ReplyClassFor(r.Kind, r.Intent)
	if class == replyclassify.ClassUnknown && IsAutomatedKind(r.Kind) {
		// A bounce or a notice is machine mail, as the inbox files it.
		class = replyclassify.ClassAutoReply
	}
	if class == replyclassify.ClassUnknown {
		return unknown
	}
	conf := r.KindConfidence
	if r.Kind == KindHumanReply {
		conf = r.IntentConfidence
	}
	return replyclassify.Result{Class: class, Confidence: conf, Source: replyclassify.SourceModel}
}
