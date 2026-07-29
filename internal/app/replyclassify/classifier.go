// Package replyclassify classifies an inbound email reply into a small, stable
// set of classes used by campaign reply automation (reply branching, the
// OOO-aware stop_on_reply check, and CRM actions).
//
// Classification is layered, cheapest-first, and the cheap layers are pure:
//
//	Layer 1 (headers): deterministic, offline. RFC 3834 auto-submission markers,
//	        Precedence: bulk/auto_reply, vendor auto-responder headers, null
//	        Return-Path, delivery-status reports, mailer-daemon senders, and the
//	        well-known "Automatic reply" / "Out of Office" subjects.
//	Layer 2 (lexicon): deterministic, offline keyword scan. Compliance words
//	        (unsubscribe / stop / remove me) win first; then clear interest
//	        phrases (positive) and clear rejection (negative).
//	Layer 3 (model): OPTIONAL, gated by the AI provider. Only the ambiguous middle
//	        reaches it. When no provider is configured we NEVER call out and fall back to
//	        "unknown".
//
// Layers 1 and 2 are pure and deterministic; only Layer 3 has side effects and
// only when explicitly configured.
package replyclassify

import (
	"context"
	"strings"
)

// Reply class enum. These exact strings are the shared contract: they are stored
// on campaign_contact_progress.reply_class and matched by the reply_* branch
// conditions in the campaign editor. Keep in sync with migration 000027's CHECK
// constraint and the frontend.
const (
	ClassPositive    = "positive"
	ClassNegative    = "negative"
	ClassNeutral     = "neutral"
	ClassAutoReply   = "auto_reply"
	ClassOutOfOffice = "out_of_office"
	ClassUnsubscribe = "unsubscribe"
	ClassUnknown     = "unknown"
)

// Source enum: which layer produced the verdict. Stored in
// campaign_contact_progress.reply_source. "" means unclassified.
const (
	SourceHeader  = "header"
	SourceLexicon = "lexicon"
	SourceModel   = "model"
)

// Input is the minimal projection of an inbound reply the classifier needs.
// Headers is the raw header map (canonical or lower-cased keys both tolerated);
// Subject and BodyText are best-effort plain text (snippet is fine).
type Input struct {
	Headers  map[string][]string
	Subject  string
	BodyText string
}

// Result is the classifier verdict. Confidence is in [0,1]. Source names the
// layer that decided.
type Result struct {
	Class      string
	Confidence float64
	Source     string
}

// Classify runs the layered pipeline. Layers 1-2 are pure/deterministic and
// always run offline. Layer 3 (the model) runs only when a provider-backed
// classifier is configured; otherwise the ambiguous middle resolves to
// "unknown" without any network call.
//
// Classify uses context.Background for the optional model call so existing pure
// callers don't need a ctx. Use ClassifyContext to pass a deadline.
func Classify(in Input) Result {
	return ClassifyContext(context.Background(), in)
}

// ClassifyContext is Classify with an explicit context for the optional model
// layer. Layers 1-2 ignore the context entirely (no I/O). The model layer is
// always allowed (subject only to the AI-provider gate).
func ClassifyContext(ctx context.Context, in Input) Result {
	return ClassifyGated(ctx, in, nil)
}

// ModelGate decides, lazily, whether the optional Layer-3 model call is worth
// making. It is consulted ONLY after Layers 1-2 were inconclusive (the cheap,
// free path already short-circuits auto-replies, OOO, bounces, unsubscribes and
// clear sentiment), so returning false caps AI spend on a reply flood by falling
// back to "unknown". The caller supplies the policy (e.g. skip if this contact
// was already classified, or an org budget is exhausted).
type ModelGate func() bool

// ClassifyGated is ClassifyContext with a gate on the optional model layer.
// gate == nil behaves exactly like the ungated pipeline (model allowed whenever
// the model classifier is configured).
func ClassifyGated(ctx context.Context, in Input, gate ModelGate) Result {
	// Layer 1: headers (deterministic, offline). An automated message is decided
	// here and never reaches the lexicon/model layers.
	if r, ok := classifyHeaders(in); ok {
		return r
	}

	// Layer 2: lexicon (deterministic, offline). Compliance words win, then
	// clear interest / rejection.
	if r, ok := classifyLexicon(in); ok {
		return r
	}

	// Layer 3: model (optional, provider-gated AND cost-gated). Skipped
	// entirely when the gate declines, with no network call.
	if gate == nil || gate() {
		if r, ok := classifyModel(ctx, in); ok {
			return r
		}
	}

	return Result{Class: ClassUnknown, Confidence: 0, Source: ""}
}

// WorthModeling is a cheap content-sanity pre-check for the model layer: a reply
// with too little human text to carry sentiment the lexicon already missed is not
// worth a paid classification (trivial "ok"/"thanks" acks, empty/whitespace
// bodies). Use it inside a ModelGate.
func WorthModeling(in Input) bool {
	return len([]rune(strings.TrimSpace(in.BodyText))) >= 12
}

// IsAutomated reports whether a stored reply_class represents an automated
// (non-human) reply. This is the single source of truth for the "OOO trap":
// stop_on_reply and the plain "replied" branch condition must NOT treat these as
// a human reply.
func IsAutomated(class string) bool {
	return class == ClassAutoReply || class == ClassOutOfOffice
}
