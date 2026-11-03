package replyclassify

import "testing"

// A reply that is only an opt-out command opts out; the same word inside a
// sentence does not.
func TestIsOptOutCommandReply(t *testing.T) {
	optOut := map[string]string{
		"bare":            "STOP",
		"lower":           "stop",
		"period":          "Stop.",
		"bangs":           "STOP!!",
		"please after":    "stop please",
		"please before":   "Please stop.",
		"remove":          "Remove",
		"remove me":       "remove me!",
		"unsubscribe":     "UNSUBSCRIBE",
		"opt out":         "Opt out",
		"optout":          "optout",
		"german":          "Stopp bitte",
		"spanish":         "Baja",
		"iphone footer":   "Stop\n\nSent from my iPhone",
		"outlook footer":  "STOP\r\n\r\nGet Outlook for Android",
		"sign-off":        "Stop.\n\nThanks,\nJohn",
		"signature":       "Stop\n\nJohn Smith\nVP Sales, Acme Inc\n+1 555 123 4567",
		"delimiter":       "stop\n-- \nJohn Smith\nThis line is a long company tagline that goes on and on about the company",
		"greeting":        "Hi Jane,\n\nStop.\n\nBest,\nJohn",
		"quoted history":  "STOP\n\nOn Tue, Sep 1, 2026 Jane <jane@example.com> wrote:\n> Do you have ten minutes next week?",
		"crlf":            "Stop\r\n",
		"leading blank":   "\n\n  stop  \n",
		"thanks after":    "Remove, thanks",
		"repeated":        "stop stop stop",
		"disclaimer foot": "Stop\n\nThis email and any attachments are confidential and intended solely for the addressee.",
		"full signature":  "STOP\n\nThanks,\nJohn Smith\nSent from my iPhone",
		"dash delimiter":  "Stop.\n--\nJane Doe | Acme",
		"title and phone": "Stop\n\nBest regards,\nJane Doe\nHead of Sales, Acme Inc.\n+49 (30) 1234-567\njane@acme.example",
		"german footer":   "Stopp\n\nVon meinem iPhone gesendet",
		"notice":          "Remove\n\nCONFIDENTIALITY NOTICE: The contents of this message are private.",
	}
	for name, body := range optOut {
		if !IsOptOut("Re: Quick question", body) {
			t.Errorf("%s: IsOptOut(%q) = false, want true", name, body)
		}
		if !MentionsOptOut("Re: Quick question", body) {
			t.Errorf("%s: MentionsOptOut(%q) = false, want true", name, body)
		}
		if r, ok := classifyLexicon(Input{BodyText: body}); !ok || r.Class != ClassUnsubscribe {
			t.Errorf("%s: classifyLexicon(%q) = %+v, want unsubscribe", name, body, r)
		}
	}

	notOptOut := map[string]string{
		"booth":            "Stop by our booth next week!",
		"stop by":          "stop by tomorrow",
		"cant stop":        "I can't stop thinking about this.",
		"attachment":       "remove the attachment",
		"wrapped sentence": "Stop\nby our booth next week",
		"prose under":      "Stop\n\nActually I'd love a demo",
		"question under":   "Stop.\n\nCan we talk next week?",
		"stop sign":        "Stop sign",
		"trailing word":    "Thanks for reaching out. Stop",
		"stop the press":   "Stop the press, this is great",
		"empty":            "",
		"greeting only":    "Hi Jane,",
		"bus stop":         "bus stop",
		"long text below":  "Stop\n\nWe looked at this last quarter and decided to build it in house instead",
		"thanks lets talk": "Stop\nThanks, let's talk.",
		"great deal":       "Stop\nGreat deal, let's talk.",
		"prose after sign": "Stop\nThanks,\nActually I'd love a demo",
		"this email prose": "Stop\nThis email is great, call me",
		"confidential":     "Stop\nConfidential: we are evaluating vendors now",
		"disclaimer word":  "Stop\nDisclaimer I want a demo",
		"sign-off prose":   "Stop\nBest time to call is Monday",
		"greeting prose":   "Hey can we talk\nStop",
		"name then prose":  "Stop\n\nJohn\nwhen are you free",
	}
	for name, body := range notOptOut {
		if IsOptOut("Re: Stop", body) {
			t.Errorf("%s: IsOptOut(%q) = true, want false", name, body)
		}
		if MentionsOptOut("Re: Stop", body) {
			t.Errorf("%s: MentionsOptOut(%q) = true, want false", name, body)
		}
		if r, ok := classifyLexicon(Input{BodyText: body}); ok && r.Class == ClassUnsubscribe {
			t.Errorf("%s: classifyLexicon(%q) = %+v, want no unsubscribe", name, body, r)
		}
	}
}
