package msgraph

import (
	"fmt"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// GraphMessage is the subset of the Graph message resource we read. Delta pages
// return a light shape (id, isRead, @removed); a follow-up $select GET fills in
// the envelope, body, and headers.
type GraphMessage struct {
	ID                     string           `json:"id"`
	InternetMessageID      string           `json:"internetMessageId"`
	ConversationID         string           `json:"conversationId"`
	Subject                string           `json:"subject"`
	BodyPreview            string           `json:"bodyPreview"`
	IsRead                 bool             `json:"isRead"`
	ReceivedDateTime       time.Time        `json:"receivedDateTime"`
	From                   *graphRecipient  `json:"from"`
	Sender                 *graphRecipient  `json:"sender"`
	ToRecipients           []graphRecipient `json:"toRecipients"`
	CcRecipients           []graphRecipient `json:"ccRecipients"`
	BccRecipients          []graphRecipient `json:"bccRecipients"`
	ReplyTo                []graphRecipient `json:"replyTo"`
	Body                   *graphItemBody   `json:"body"`
	InternetMessageHeaders []graphHeader    `json:"internetMessageHeaders"`

	// Removed is set (with a reason) on delta items that were deleted or moved
	// out of the tracked folder.
	Removed *graphRemoved `json:"@removed"`
}

type graphRecipient struct {
	EmailAddress graphEmailAddress `json:"emailAddress"`
}

type graphEmailAddress struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type graphItemBody struct {
	ContentType string `json:"contentType"` // "html" | "text"
	Content     string `json:"content"`
}

type graphHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type graphRemoved struct {
	Reason string `json:"reason"`
}

// header returns the first internet header matching name (case-insensitive).
func (m *GraphMessage) header(name string) string {
	for _, h := range m.InternetMessageHeaders {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// toEmailData maps a fully-hydrated Graph message onto the internal
// EmailMessageData. The opaque Graph message id is carried in GmailID (the
// provider-message-id field), and ConversationID stands in for the thread id.
func (m *GraphMessage) toEmailData() *models.EmailMessageData {
	var plain, html string
	if m.Body != nil {
		if strings.EqualFold(m.Body.ContentType, "html") {
			html = m.Body.Content
		} else {
			plain = m.Body.Content
		}
	}

	flags := []string{}
	if m.IsRead {
		flags = append(flags, "\\Seen")
	}

	// Surface the warmup verification token as a pseudo-flag ("Header:value").
	// The consumer's warmup detector reads it from Flags to identify warmup mail
	// and file it into the Warmbly folder instead of the inbox.
	if tok := m.header(config.WarmupVerifyHeader); tok != "" {
		flags = append(flags, config.WarmupVerifyHeader+":"+tok)
	}
	// Surface machine-reply / DSN-bounce markers as pseudo-flags so the
	// consumer's reply/bounce classifier can read them (the sync model has no
	// arbitrary-header field).
	for _, name := range config.InboundClassificationHeaders {
		if v := strings.TrimSpace(m.header(name)); v != "" {
			flags = append(flags, name+":"+v)
		}
	}

	return &models.EmailMessageData{
		GmailID:      m.ID,
		ThreadID:     m.ConversationID,
		MessageID:    m.InternetMessageID,
		Snippet:      m.BodyPreview,
		Subject:      m.Subject,
		From:         recipients(oneRecipient(m.From)),
		Sender:       recipients(oneRecipient(m.Sender)),
		To:           recipients(m.ToRecipients),
		CC:           recipients(m.CcRecipients),
		BCC:          recipients(m.BccRecipients),
		ReplyTo:      recipients(m.ReplyTo),
		InReplyTo:    splitHeaderList(m.header("In-Reply-To")),
		Date:         m.ReceivedDateTime,
		InternalDate: m.ReceivedDateTime,
		Flags:        flags,
		BodyPlain:    plain,
		BodyHTML:     html,
	}
}

func oneRecipient(r *graphRecipient) []graphRecipient {
	if r == nil {
		return nil
	}
	return []graphRecipient{*r}
}

// recipients renders Graph recipients as RFC 5322 address strings, matching the
// "Name <addr>" shape the Gmail path produces.
func recipients(in []graphRecipient) []string {
	var out []string
	for _, r := range in {
		addr := r.EmailAddress.Address
		if addr == "" {
			continue
		}
		if r.EmailAddress.Name != "" {
			out = append(out, fmt.Sprintf("%s <%s>", r.EmailAddress.Name, addr))
		} else {
			out = append(out, addr)
		}
	}
	return out
}

func splitHeaderList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
