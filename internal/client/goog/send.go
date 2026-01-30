package goog

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"google.golang.org/api/gmail/v1"
)

func (c *Client) SendMessage(
	ctx context.Context,
	to, cc, bcc []string,
	messageID,
	subject, bodyPlain, bodyHTML string,
	parent *models.EmailMessageData,
	customHeaders ...map[string]string,
) (*gmail.Message, error) {
	// Compose headers
	headers := []*gmail.MessagePartHeader{
		{Name: "From", Value: c.GetAddress()},
		{Name: "To", Value: strings.Join(to, ", ")},
		{Name: "Subject", Value: subject},
		{Name: "Message-ID", Value: messageID},
	}

	if len(cc) > 0 {
		headers = append(headers, &gmail.MessagePartHeader{
			Name:  "Cc",
			Value: strings.Join(cc, ", "),
		})
	}

	if len(bcc) > 0 {
		headers = append(headers, &gmail.MessagePartHeader{
			Name:  "Bcc",
			Value: strings.Join(bcc, ", "),
		})
	}

	if parent != nil && parent.MessageID != "" {
		headers = append(headers,
			&gmail.MessagePartHeader{Name: "In-Reply-To", Value: fmt.Sprintf("<%s>", parent.MessageID)},
			&gmail.MessagePartHeader{Name: "References", Value: fmt.Sprintf("<%s>", parent.MessageID)},
		)
	}

	// Add custom headers (e.g., X-Warmbly-Token for warmup)
	if len(customHeaders) > 0 {
		for k, v := range customHeaders[0] {
			headers = append(headers, &gmail.MessagePartHeader{Name: k, Value: v})
		}
	}

	// Compose parts
	var parts []*gmail.MessagePart

	// Plain text part
	parts = append(parts, &gmail.MessagePart{
		MimeType: "text/plain",
		Body: &gmail.MessagePartBody{
			Data: base64.URLEncoding.EncodeToString([]byte(bodyPlain)),
		},
	})

	// HTML part (optional)
	if bodyHTML != "" {
		parts = append(parts, &gmail.MessagePart{
			MimeType: "text/html",
			Body: &gmail.MessagePartBody{
				Data: base64.URLEncoding.EncodeToString([]byte(bodyHTML)),
			},
		})
	}

	// Full message
	msg := &gmail.Message{
		Payload: &gmail.MessagePart{
			MimeType: "multipart/alternative",
			Headers:  headers,
			Parts:    parts,
		},
	}

	// Threading
	if parent != nil && parent.ThreadID != "" {
		msg.ThreadId = parent.ThreadID
	}

	// Send via Gmail API
	sent, err := c.srv.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return nil, fmt.Errorf("send message failed: %w", err)
	}

	return sent, nil
}
