package slackapp

// Block Kit limits the builders respect.
const (
	maxSectionText  = 3000
	maxButtonText   = 75
	maxFallbackText = 3000
	// maxAnswerRunes keeps one markdown block under Slack's 12,000 limit.
	maxAnswerRunes = 11000
)

func plainText(s string) Block {
	return Block{"type": "plain_text", "text": s, "emoji": true}
}

func mrkdwnText(s string) Block {
	return Block{"type": "mrkdwn", "text": truncateRunes(s, maxSectionText)}
}

func sectionBlock(mrkdwn string) Block {
	return Block{"type": "section", "text": mrkdwnText(mrkdwn)}
}

func contextBlock(mrkdwn string) Block {
	return Block{"type": "context", "elements": []any{mrkdwnText(mrkdwn)}}
}

func markdownBlock(md string) Block {
	return Block{"type": "markdown", "text": md}
}

func headerBlock(s string) Block {
	return Block{"type": "header", "text": plainText(truncateRunes(s, 150))}
}

func actionsBlock(elements ...Block) Block {
	els := make([]any, 0, len(elements))
	for _, e := range elements {
		if e != nil {
			els = append(els, e)
		}
	}
	return Block{"type": "actions", "elements": els}
}

// urlButton opens a link; Slack still posts a block_action, which is acked.
func urlButton(text, url string) Block {
	if url == "" {
		return nil
	}
	return Block{"type": "button", "text": plainText(truncateRunes(text, maxButtonText)), "url": url, "action_id": ActionOpenURL + ":" + text}
}

func actionButton(text, actionID, value, style string) Block {
	b := Block{"type": "button", "text": plainText(truncateRunes(text, maxButtonText)), "action_id": actionID, "value": value}
	if style != "" {
		b["style"] = style
	}
	return b
}

// buttonsBlock returns an actions block, or nil when no button survived.
func buttonsBlock(elements ...Block) Block {
	b := actionsBlock(elements...)
	if len(b["elements"].([]any)) == 0 {
		return nil
	}
	return b
}

// blocks drops nil entries.
func blocks(in ...Block) []Block {
	out := make([]Block, 0, len(in))
	for _, b := range in {
		if b != nil {
			out = append(out, b)
		}
	}
	return out
}

// linkPromptFor is linkPrompt; held says a question waits for the link.
func linkPromptFor(linkURL, lead string, held bool) Message {
	if lead == "" {
		lead = "Link your Warmbly account so I can answer as you, with your workspace permissions."
	}
	if linkURL == "" {
		return Message{Text: lead, Blocks: blocks(sectionBlock(lead + " Ask your Warmbly admin to set the dashboard address (APP_URL) for this instance."))}
	}
	expiry := "The link works once and expires in 15 minutes."
	if held {
		expiry += " I'll answer your question as soon as you've linked."
	}
	return Message{
		Text: lead,
		Blocks: blocks(
			sectionBlock(lead),
			buttonsBlock(urlButton("Link your Warmbly account", linkURL)),
			contextBlock(expiry),
		),
	}
}

// plainMessage is a one-section message with a matching fallback.
func plainMessage(mrkdwn string) Message {
	return Message{Text: truncateRunes(mrkdwn, maxFallbackText), Blocks: blocks(sectionBlock(mrkdwn))}
}
