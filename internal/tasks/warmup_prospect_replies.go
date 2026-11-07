package tasks

import (
	"errors"
	"hash/fnv"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
	"github.com/warmbly/warmbly/internal/repository"
)

// prospectExchange is one way a person answers a cold email, and the ways a
// sender answers that. Every line fits any opening, so none invents a fact
// about the copy it answers.
type prospectExchange struct {
	replies []string
	answers []string
}

// prospectExchanges continue a thread opened with campaign copy the way a
// prospect and the sender would. A thread draws one exchange and one line of
// each turn from its opening's Message-ID, so threads differ from each other
// while a retry of the same turn writes the same words.
var prospectExchanges = []prospectExchange{
	{ // Wants to know more.
		replies: []string{
			"Thanks for reaching out. Could you send over a bit more detail on how this would work for us?",
			"Hi,\n\nThis caught my eye. Do you have a short overview I could read before we go any further?",
			"Interesting timing, we were talking about this last week. What would getting started look like?",
			"Happy to take a look. Can you send something I can forward around internally?",
			"Thanks. I'd like to understand this better before saying yes or no. Anything written you can share?",
			"Hi,\n\nCould be relevant for us. What does a typical first month look like?",
		},
		answers: []string{
			"Thanks for getting back to me. I'll put together a short overview and send it across this week.",
			"Glad it's relevant. I'll send a one-page summary shortly, and you can tell me which parts matter to you.",
			"Appreciate the quick reply. I'll write up how the first few weeks usually go and send it over.",
			"Of course. I'll keep it short so it's easy to pass around, and send it later today.",
			"Thanks, that helps. I'll send a brief write-up and you can decide from there whether a call makes sense.",
		},
	},
	{ // Not now.
		replies: []string{
			"Thanks for the note. Timing isn't great for us this month. Could you check back in a few weeks?",
			"Hi,\n\nWe're heads down on a launch right now, so I can't give this proper attention. Maybe next quarter?",
			"Appreciate it, but we've just locked our plans for the next couple of months. Feel free to follow up later.",
			"Not a priority for us at the moment, though that could change after the summer.",
			"Thanks. Budget is spoken for until the new year, so I'd rather not start something now.",
		},
		answers: []string{
			"Understood, thanks for letting me know. I'll check back in a few weeks.",
			"Makes sense, good luck with the launch. I'll follow up once things have settled down.",
			"No problem at all. I'll leave it for now and reach out again later in the year.",
			"Thanks for the honest answer. I'll make a note to get back in touch next quarter.",
			"Totally fair. I'll stay out of your inbox until then.",
		},
	},
	{ // Not a fit.
		replies: []string{
			"Thanks, but this isn't something we need right now.",
			"Hi,\n\nI appreciate you thinking of us, but we're not looking at anything in this area.",
			"Thanks for reaching out. We're a small team and this doesn't really fit how we work.",
			"Not for us, but thanks for the clear email. Most of these are much longer.",
			"We'll pass for now. Thanks for keeping it short.",
		},
		answers: []string{
			"Thanks for letting me know, I appreciate the reply. I'll leave it there.",
			"Understood, and thanks for taking the time to answer. All the best.",
			"No problem, thanks for being direct. I won't follow up on this.",
			"Appreciate the quick answer. If anything changes, you know where to find me.",
			"Fair enough, thanks for reading it. Have a good rest of the week.",
		},
	},
	{ // Asks how it would work for them.
		replies: []string{
			"How does this usually work for a team our size? We're around twenty people.",
			"Hi,\n\nWhat's involved on our side if we try this? I'm wary of anything that takes a lot of setup.",
			"Quick question before anything else: how long does it usually take before people see a difference?",
			"Thanks for the email. Who on our side would normally own this?",
			"Curious how this fits with what we already use. Does it replace anything or sit alongside it?",
		},
		answers: []string{
			"Good question. It depends a little on the setup, so I'll send a short note covering the usual cases.",
			"Fair thing to ask. Setup on your side is usually small, and I'll spell out exactly what's involved in a follow-up.",
			"Thanks for asking. I'll write up what the first few weeks look like so you can judge it properly.",
			"Usually one person keeps an eye on it. I'll send a short explanation of how teams split the work.",
			"It normally sits alongside what you have. I'll send a quick outline so you can see where it fits.",
		},
	},
	{ // Open to a short call.
		replies: []string{
			"Sure, I'm open to a short call. What does your week look like?",
			"Hi,\n\nHappy to chat for fifteen minutes. Thursday afternoon could work on my side.",
			"Could be worth a quick conversation. Send me a couple of times that suit you.",
			"Let's talk. Early next week is easiest for me.",
			"Thanks. A short call is fine, just keep it to twenty minutes if you can.",
		},
		answers: []string{
			"Great, thanks. I'll send over a couple of times and you can pick whichever suits.",
			"Thanks for making time. I'll send an invite and keep it short.",
			"Appreciate it. I'll suggest two slots and you can choose.",
			"Sounds good. I'll send a calendar invite shortly and keep it brief.",
			"Thanks. I'll send a few options and we can go from there.",
		},
	},
	{ // Already covered.
		replies: []string{
			"We already have something in place for this. What would be different with you?",
			"Hi,\n\nThanks, but we're fairly happy with our current setup. Is there a reason to look again?",
			"We looked at this area last year and picked another option. Has much changed since?",
			"Our current approach works well enough. I'd need a clear reason to switch.",
		},
		answers: []string{
			"That's fair. I'll send a short note on where teams usually see a difference, and you can judge whether it's worth a look.",
			"Glad it's working for you. I'll leave a brief comparison with you in case it's ever useful.",
			"Thanks for the context. I'll keep it short and only point out what might matter to you.",
			"Understood. If the current setup keeps working, there's no reason to change. I'll leave it with you.",
		},
	},
}

// prospectLine picks the line a thread's turn uses from its opening's Message-ID.
func prospectLine(root string, turn int) (string, bool) {
	if root == "" || turn < 1 || turn > 2 {
		return "", false
	}
	pick := func(salt string, n int) int {
		h := fnv.New32a()
		_, _ = h.Write([]byte(salt + "\x00" + root))
		return int(h.Sum32() % uint32(n))
	}
	ex := prospectExchanges[pick("exchange", len(prospectExchanges))]
	if turn == 1 {
		return ex.replies[pick("reply", len(ex.replies))], true
	}
	return ex.answers[pick("answer", len(ex.answers))], true
}

// threadRoot is the opening's Message-ID, bare as references store it: the first reference, or the parent itself when it is the opening.
func threadRoot(c *repository.WarmupReplyCandidate) string {
	root := c.MessageID
	if len(c.References) > 0 {
		root = c.References[0]
	}
	return strings.Trim(strings.TrimSpace(root), "<>")
}

// campaignReplyContent writes the next turn of a thread opened with campaign
// copy: the recipient answers as a prospect would, then the sender replies.
func campaignReplyContent(c *repository.WarmupReplyCandidate, account Email) (warmupContent, string, []string, error) {
	if c == nil || c.ConversationID != nil || c.MessageID == "" || c.Subject == "" || c.ConversationTurn+1 >= c.MaxTurns || c.RenderingVersion != generation.CanonicalRenderingVersion {
		return warmupContent{}, "", nil, errors.New("campaign copy thread unavailable")
	}
	line, ok := prospectLine(threadRoot(c), c.ConversationTurn+1)
	if !ok {
		return warmupContent{}, "", nil, errors.New("campaign copy thread exhausted")
	}
	signature := account.Name
	if signature == "" {
		signature = account.Email
	}
	body, err := generation.RenderCanonicalTurn(line, signature)
	if err != nil {
		return warmupContent{}, "", nil, err
	}
	refs, err := mailhdr.ReferenceIDs(append(append([]string{}, c.References...), c.MessageID))
	if err != nil {
		return warmupContent{}, "", nil, err
	}
	return warmupContent{subject: c.Subject, body: body, theme: c.ConversationTheme, contentSource: models.WarmupContentSourceCampaign, scenarioVersion: c.ScenarioVersion, renderingVersion: c.RenderingVersion, maxTurns: c.MaxTurns}, c.MessageID, refs, nil
}
