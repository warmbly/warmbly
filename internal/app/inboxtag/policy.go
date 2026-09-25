// Package inboxtag classifies inbound mail into workspace labels and a
// relevance score, using the TypeSafe "Jev" model for the judgments and code
// for everything else.
//
// THIS FILE IS THE POLICY. Every question asked of the model, every threshold,
// and every weight lives here, so the whole policy can be reviewed without
// reading a line of application logic. Nothing below this file decides what a
// label means or when one is applied.
//
// Three rules shape the design, and each one exists because breaking it
// produced a wrong answer in testing:
//
//  1. One call per email. Questions run in parallel against one state ingest,
//     which avoids repeated requests and duplicated state tokens.
//
//  2. The model tags, code decides. No answer here reaches a side effect on its
//     own. decide() in decide.go is the only thing that turns answers into
//     labels, and in this phase labels are all it may produce.
//
//  3. Never ask the model what the system already knows. Direction, sender,
//     campaign context and deterministic automated-message signals are facts.
//     Given only a body, Jev called our own outbound a human reply at 0.94
//     confidence: confidently wrong, and confidence cannot save you from a
//     question that should never have been asked.
package inboxtag

import (
	"sort"

	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// Model is the pinned model every threshold below was calibrated against. The
// pin itself lives with the shared client, so every feature asks one version.
const Model = typesafe.Model

// Thresholds. Tuning these is tuning the product.
const (
	// ConfFloor is the confidence below which a Choice is not acted on at all.
	// A low-confidence answer is not merely uncertain, it is not reproducible:
	// the same ambiguous input run three times returned a different winning
	// label each time while confidence stayed near 0.2. Below this the thread
	// is labelled needs-review and nothing else happens.
	ConfFloor = 0.70

	// Yes is the noul level at which a signal counts as present.
	Yes = 0.60

	// Strong is required before anything that would pause a sequence or
	// suppress an address. Nothing in this phase acts on it; it is here because
	// the phase-2 and phase-3 rules must read the same number as the review
	// page a human used to decide those phases were safe.
	Strong = 0.80
)

// BodyLimit caps the plain-text body sent as state. Accuracy falls as state
// grows with content the question does not need, and these threads carry long
// quoted chains. Quoting is stripped before this applies; the cap is for the
// genuinely long message.
const BodyLimit = 4000

// ── Taxonomy ───────────────────────────────────────────────────────────────
// Changing any identifier here changes stored history and the labels a
// workspace has already filed against. Treat these as a contract.

// Kind is the mutually exclusive question: what this message IS.
const (
	KindBounceHard      = "bounce_hard"
	KindBounceSoft      = "bounce_soft"
	KindAutoReplyOOO    = "auto_reply_ooo"
	KindAutoReplyTicket = "auto_reply_ticket"
	KindHumanReply      = "human_reply"
	KindColdInbound     = "cold_inbound"
	KindNotification    = "notification"
	KindInternal        = "internal"
)

var kindCriteria = map[string]string{
	KindBounceHard:      "Permanent delivery failure; the address does not exist",
	KindBounceSoft:      "Temporary delivery failure: mailbox full, greylisted, server busy",
	KindAutoReplyOOO:    "Out-of-office or vacation autoresponder",
	KindAutoReplyTicket: "Automated ticket receipt or \"we got your message\" acknowledgement",
	KindHumanReply:      "A real person responding to our outreach",
	KindColdInbound:     "Someone cold-pitching us; not a reply to our campaign",
	KindNotification:    "Automated message from a service, platform or mailing list: security alerts, sign-in and verification codes, account, billing and system notices, receipts, newsletters",
	KindInternal:        "From our own team or forwarded internally",
}

// Intent travels in the same request but is read only when the kind is a human
// reply. Reading it on a bounce would use an answer with no subject.
const (
	IntentAgreed        = "agreed"
	IntentWantsInfo     = "wants_info"
	IntentWantsPricing  = "wants_pricing"
	IntentNotNow        = "not_now"
	IntentNotInterested = "not_interested"
	IntentWrongPerson   = "wrong_person"
	IntentOptOut        = "opt_out"
	IntentUnclear       = "unclear"

	// Mid-conversation intents. The seven above describe a first answer to cold
	// outreach; these describe a thread that is already a working relationship,
	// which is most of a real inbox once anything has been agreed.
	//
	// Added after reading the replies that scored lowest. They were not
	// ambiguous: "Would 2pm on the 22nd work?", "We have updated everything on
	// our end", "For the guest post you will write an article to our
	// guidelines" are all perfectly clear, and the model was splitting
	// probability between `agreed` and `wants_info` because neither was true.
	// Low confidence there was a missing bucket, not a hard message.
	IntentScheduling       = "scheduling"
	IntentInProgress       = "in_progress"
	IntentQuestionAnswered = "question_answered"
)

var intentCriteria = map[string]string{
	IntentAgreed:           "Clearly agrees to the partnership, call, or next step",
	IntentWantsInfo:        "Open, but asking questions before deciding",
	IntentWantsPricing:     "Specifically asking about price, terms, or commercials",
	IntentNotNow:           "Open in principle, says the timing is wrong",
	IntentNotInterested:    "Declines, without demanding removal",
	IntentWrongPerson:      "Says they are not the right contact, or names someone else",
	IntentOptOut:           "Demands removal, complains, or threatens",
	IntentScheduling:       "Proposes, confirms, or changes a specific time to meet or talk",
	IntentInProgress:       "Reports progress on something already agreed, or says their side is done",
	IntentQuestionAnswered: "Answers a question we asked, or supplies information we requested",
	// Deliberate: somewhere to put a genuinely ambiguous reply, so the model is
	// never forced to pick a wrong bucket to answer at all.
	IntentUnclear: "A reply whose intent cannot be determined from the text",
}

// Signals co-occur, so each is its own yes/no. Each is one plain literal
// statement: Jev reads instructions literally, so no double negatives, no
// "unless", and never two judgments in one question.
const (
	SigAsksForCall           = "asks_for_call"
	SigAsksTechnicalQuestion = "asks_technical_question"
	SigMentionsPricing       = "mentions_pricing"
	SigMentionsTimeline      = "mentions_timeline"
	SigNamesAnotherContact   = "names_another_contact"
	SigRequestsRemoval       = "requests_removal"
	SigLegalThreat           = "legal_threat"
	SigHostileTone           = "hostile_tone"
	SigMentionsCompetitor    = "mentions_competitor"
	SigAlreadyUsingSimilar   = "already_using_similar"
	SigMentionsShopify       = "mentions_shopify"
	SigOffersSomethingBack   = "offers_something_back"
	SigIsDecisionMaker       = "is_decision_maker"
	SigContainsSchedulingLnk = "contains_scheduling_link"
	SigNeedsHumanJudgement   = "needs_human_judgement"
)

var signalInstructions = map[string]string{
	SigAsksForCall:           "The sender asks to have a call, a meeting, or a demo.",
	SigAsksTechnicalQuestion: "The sender asks a question about how the product works.",
	SigMentionsPricing:       "The sender mentions price, cost, fees, or commercial terms.",
	SigMentionsTimeline:      "The sender mentions a date, a deadline, or a period of time for deciding or acting.",
	SigNamesAnotherContact:   "The sender names a different person to contact.",
	SigRequestsRemoval:       "The sender asks to be removed from the list or to stop receiving email.",
	SigLegalThreat:           "The sender threatens legal action or names a regulator.",
	SigHostileTone:           "The sender is angry, rude, or insulting.",
	SigMentionsCompetitor:    "The sender names a competing product or vendor.",
	SigAlreadyUsingSimilar:   "The sender says they already use a product that does this.",
	SigMentionsShopify:       "The sender mentions Shopify.",
	SigOffersSomethingBack:   "The sender offers something in return, such as a partnership, a referral, or an exchange.",
	SigIsDecisionMaker:       "The sender says they decide this, or writes as the person who owns the decision.",
	SigContainsSchedulingLnk: "The message contains a link to a scheduling or booking page.",
	SigNeedsHumanJudgement:   "Answering this message well requires a person to read it.",
}

// Scores are ordered rubrics. Ten levels is the API maximum; eleven is a 400.
// The score value is used for threshold checks only, never as a magnitude to
// do arithmetic between levels with.
const (
	ScoreHeat        = "heat"
	ScoreReplyEffort = "reply_effort"
	ScoreSentiment   = "sentiment"
	ScoreUrgency     = "urgency"
)

var scoreCriteria = map[string][]string{
	ScoreHeat:        {"No interest", "Curious only", "Actively evaluating", "Ready to commit"},
	ScoreReplyEffort: {"One-line ack", "Short answer", "Needs a real write-up", "Needs a call"},
	ScoreSentiment:   {"Hostile", "Cold", "Neutral", "Warm", "Enthusiastic"},
	ScoreUrgency:     {"No deadline", "Soon", "This week", "Today"},
}

var scoreInstructions = map[string]string{
	ScoreHeat:        "How close is the sender to committing to the next step?",
	ScoreReplyEffort: "How much work is a good reply to this message?",
	ScoreSentiment:   "How does the sender feel about us?",
	ScoreUrgency:     "How soon does the sender need an answer?",
}

// ── Relevance ──────────────────────────────────────────────────────────────
// Computed in code from the answers, never asked. Asking a composite "how well
// does this match?" score returns a flat distribution at confidence 0.00: the
// model correctly refusing a question that hides four independent judgments.

// Weights are points added to relevance when the named condition holds. The
// keys are intents, signals, scores and kind families, resolved by decide().
var Weights = map[string]float64{
	IntentAgreed: 50,
	// A proposed time is further along than an agreement in principle: someone
	// naming a slot has already decided, and missing it costs the meeting.
	IntentScheduling:   45,
	IntentWantsPricing: 35,
	IntentWantsInfo:    30,
	// Work in flight still needs answering, but it is not a decision waiting on
	// us, so it sits below the intents that are.
	IntentQuestionAnswered: 20,
	IntentInProgress:       15,
	SigAsksForCall:         25,
	SigIsDecisionMaker:     15,
	ScoreUrgency:           10, // multiplied by the normalised score, 0..1
	ScoreHeat:              8,  // multiplied by the normalised score, 0..1
	SigHostileTone:         -20,
	IntentNotNow:           -25,
	IntentNotInterested:    -40,
	"auto_reply":           -60, // either auto_reply_* kind
	"bounce":               -80, // either bounce_* kind
}

// Priority buckets, applied to the clamped 0-100 relevance.
const (
	PriorityNow      = "now"
	PriorityToday    = "today"
	PriorityWhenever = "whenever"
	PriorityIgnore   = "ignore"
)

const (
	priorityNowAt      = 70
	priorityTodayAt    = 40
	priorityWheneverAt = 15
)

// bucket maps a relevance score to its priority. The only place the cut-offs
// are read.
func bucket(relevance float64) string {
	switch {
	case relevance >= priorityNowAt:
		return PriorityNow
	case relevance >= priorityTodayAt:
		return PriorityToday
	case relevance >= priorityWheneverAt:
		return PriorityWhenever
	default:
		return PriorityIgnore
	}
}

// LabelNeedsReview is applied when a Choice came back below ConfFloor. It is
// the one label that means "the system declined to decide".
const LabelNeedsReview = "Needs review"

// labelTitles is the label a person reads for each answer, in plain words a
// non-native speaker understands. An answer missing here is recorded but never
// becomes a label, because it would sit on most threads and filter nothing.
// Titles are what workspaces file under, so renaming one needs a migration.
var labelTitles = map[string]string{
	KindBounceHard:      "Bounced",
	KindBounceSoft:      "Bounced",
	KindAutoReplyOOO:    "Out of office",
	KindAutoReplyTicket: "Auto-reply",
	KindNotification:    "Notification",
	KindColdInbound:     "Sales pitch",

	IntentAgreed:           "Interested",
	IntentScheduling:       "Meeting",
	IntentWantsPricing:     "Pricing",
	IntentWantsInfo:        "Question",
	IntentInProgress:       "Update",
	IntentQuestionAnswered: "Update",
	IntentNotNow:           "Not now",
	IntentNotInterested:    "Not interested",
	IntentWrongPerson:      "Wrong person",
	IntentOptOut:           "Unsubscribe",

	SigAsksForCall:     "Meeting",
	SigRequestsRemoval: "Unsubscribe",
	SigLegalThreat:     "Legal threat",
}

// LabelFor is the label an answer files under, or "" when it has none.
func LabelFor(id string) string { return labelTitles[id] }

// automatedKinds are the messages no person wrote. A conversation made only of
// these leaves the inbox for the Automated view.
var automatedKinds = map[string]bool{
	KindBounceHard:      true,
	KindBounceSoft:      true,
	KindAutoReplyOOO:    true,
	KindAutoReplyTicket: true,
	KindNotification:    true,
}

// IsAutomatedKind reports whether a kind is machine-sent mail.
func IsAutomatedKind(kind string) bool { return automatedKinds[kind] }

// Questions is the entire question set, built once for one parallel request.
func Questions() map[string]Question {
	q := make(map[string]Question, len(signalInstructions)+len(scoreCriteria)+2)

	q["kind"] = Question{
		Type: QuestionChoice,
		Instructions: "Classify what this inbound email is. Judge the message itself, " +
			"not what a reply to it would say.",
		Criteria: kindCriteria,
	}

	q["intent"] = Question{
		Type: QuestionChoice,
		Instructions: "A person replied to an email we sent them. Classify what the reply " +
			"asks for, decides, or reports. The thread may already be an agreed working " +
			"relationship rather than a first answer to an approach. Judge only the new " +
			"message, not the quoted history.",
		Criteria: intentCriteria,
	}

	for id, instr := range signalInstructions {
		q[id] = Question{Type: QuestionNoul, Instructions: instr}
	}

	for id, levels := range scoreCriteria {
		q[id] = Question{
			Type:         QuestionScore,
			Instructions: scoreInstructions[id],
			Criteria:     levels,
		}
	}

	return q
}

// SignalIDs lists every noul question, so storage and the review page can
// iterate the signals without re-deriving the set.
func SignalIDs() []string {
	ids := make([]string, 0, len(signalInstructions))
	for id := range signalInstructions {
		ids = append(ids, id)
	}
	return ids
}

// ScoreIDs lists every score question, in no particular order.
func ScoreIDs() []string {
	ids := make([]string, 0, len(scoreCriteria))
	for id := range scoreCriteria {
		ids = append(ids, id)
	}
	return ids
}

// ScoreLevels is how many levels a score question declares, which is what
// normalises its answer to 0..1 for the weighted sum.
func ScoreLevels(id string) int {
	return len(scoreCriteria[id])
}

// AllLabels is every label this system can ever write, created up front so a
// workspace can filter on a label before anything has earned it.
func AllLabels() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(labelTitles)+1+len(FollowUpLabels))
	add := func(l string) {
		if l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	for _, l := range labelTitles {
		add(l)
	}
	add(LabelNeedsReview)
	// Computed rather than classified, but still labels a person filters by.
	for _, l := range FollowUpLabels {
		add(l)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) { sort.Strings(s) }

// ── Follow-up ──────────────────────────────────────────────────────────────
//
// Who owes whom a reply, and for how long. Deliberately NOT a question for the
// model: whether a message was answered and how long ago are facts in the
// database, and dates and arithmetic are the two things Jev is documented to
// be worst at. Every follow-up label below is computed from stored data, costs
// nothing, and can be recomputed as often as we like.
//
// These labels differ from the classification ones in an important way: they
// change as time passes and as people reply, so they are kept in sync rather
// than only added. A thread that was waiting yesterday and is due a
// follow-up today must not wear both.
const (
	// LabelNeedsReply: they answered and we have not. The most actionable
	// state on the page, and the easiest to lose.
	LabelNeedsReply = "Needs reply"
	// LabelFollowUp: we sent last, long enough ago to chase.
	LabelFollowUp = "Follow up"
	// LabelGoneQuiet: they were interested, then stopped answering. Its own
	// label because a stalled deal needs something different from a person
	// than an unanswered cold email.
	LabelGoneQuiet = "Gone quiet"
)

// FollowUpLabels is the set this feature keeps in sync on a thread. Only these
// are ever removed, so a label a person applied by hand is never touched.
// "We sent last and it is still early" wears nothing: that is most threads,
// and the Awaiting reply scope already lists them.
var FollowUpLabels = []string{LabelNeedsReply, LabelFollowUp, LabelGoneQuiet}

// Follow-up timing. Days, because that is the unit a person chasing a deal
// thinks in.
const (
	// OurCourtDays: how long we may sit on an inbound reply before it is worth
	// flagging. Short, because this is our own delay.
	OurCourtDays = 2
	// FollowUpDueDays: silence after our message that is worth chasing. Three
	// working days, allowing for a weekend.
	FollowUpDueDays = 5
	// GoingColdDays: silence after a POSITIVE exchange. Longer than
	// FollowUpDueDays on purpose: someone who said yes has earned more patience
	// than someone who never answered, and chasing them at day five reads as
	// pushy rather than diligent.
	GoingColdDays = 10
)

// positiveIntents are the ones that make later silence expensive. A thread that
// reached any of these was going somewhere.
var positiveIntents = map[string]bool{
	IntentAgreed:           true,
	IntentScheduling:       true,
	IntentWantsPricing:     true,
	IntentWantsInfo:        true,
	IntentQuestionAnswered: true,
	IntentInProgress:       true,
}

// closedIntents end the conversation. A thread that reached one of these is
// never chased: nagging somebody who declined is rude, and nagging somebody who
// asked to be removed is a compliance problem, not a missed opportunity.
var closedIntents = map[string]bool{
	IntentNotInterested: true,
	IntentOptOut:        true,
	IntentWrongPerson:   true,
	IntentNotNow:        true,
}

// IsPositiveIntent and IsClosedIntent are the readers, so nothing outside this
// file decides what those words mean.
func IsPositiveIntent(intent string) bool { return positiveIntents[intent] }
func IsClosedIntent(intent string) bool   { return closedIntents[intent] }
