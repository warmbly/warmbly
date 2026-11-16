package replyio

import (
	"html"
	"regexp"
	"strings"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

var dialect = func() source.Dialect {
	f := source.CommonFields()
	// Reply's sending-mailbox variables ({{SendingEmail.SenderFirstName}}) and the
	// older {{Sender.FullName}}; {{LinkedIn}} normalises to linked_in.
	f["sending_email_sender_name"] = "Sender.Name"
	f["sending_email_sender_first_name"] = "Sender.FirstName"
	f["sending_email_sender_last_name"] = "Sender.LastName"
	f["sending_email_email"] = "Sender.Email"
	f["sender_full_name"] = "Sender.Name"
	f["linked_in"] = "linkedin"
	f["time_zone_id"] = "timezone"
	return source.Dialect{Fields: f, Spin: []string{"Random"}}
}()

var tokenRE = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// translate unquotes Reply's fallbacks and spintax options
// ({{FirstName | 'there'}}, {{Random | 'Hi' | 'Hello'}}), then rewrites the tokens.
func translate(text string) (string, []string) {
	text = tokenRE.ReplaceAllStringFunc(text, func(tok string) string {
		parts := strings.Split(html.UnescapeString(tokenRE.FindStringSubmatch(tok)[1]), "|")
		for i := range parts {
			parts[i] = unquote(strings.TrimSpace(parts[i]))
		}
		return "{{" + strings.Join(parts, " | ") + "}}"
	})
	t := source.Translate(text, dialect)
	return t.Text, t.Dropped
}

func unquote(s string) string {
	r := []rune(s)
	if len(r) < 2 {
		return s
	}
	pairs := map[rune]rune{'\'': '\'', '"': '"', '‘': '’', '“': '”', '’': '’', '”': '”'}
	if closing, ok := pairs[r[0]]; ok && r[len(r)-1] == closing {
		return strings.TrimSpace(string(r[1 : len(r)-1]))
	}
	return s
}
