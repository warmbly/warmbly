package evolution

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/whatsapp"
)

// RawWebhook is a minimal Evolution webhook envelope (v2.x).
// We only extract fields needed for domain events; unknown fields are ignored.
type RawWebhook struct {
	Event    string          `json:"event"`
	Instance string          `json:"instance"`
	Data     json.RawMessage `json:"data"`
	// Some deployments nest differently
	Sender string `json:"sender,omitempty"`
	DateAt int64  `json:"date_time,omitempty"`
}

// NormalizeWebhook maps an Evolution payload into a domain ChannelEvent.
// Unsupported events return EventUnsupported with Ignored semantics for callers.
func NormalizeWebhook(raw []byte) (whatsapp.ChannelEvent, error) {
	var env RawWebhook
	if err := json.Unmarshal(raw, &env); err != nil {
		return whatsapp.ChannelEvent{}, err
	}
	ev := whatsapp.ChannelEvent{
		Channel:    whatsapp.ChannelWhatsApp,
		Provider:   whatsapp.ProviderEvolution,
		Instance:   env.Instance,
		OccurredAt: time.Now().UTC(),
		Content:    whatsapp.Content{Type: whatsapp.ContentOther},
	}

	eventName := strings.ToUpper(strings.TrimSpace(env.Event))
	eventName = strings.ReplaceAll(eventName, ".", "_")
	eventName = strings.ReplaceAll(eventName, "-", "_")

	switch eventName {
	case "MESSAGES_UPSERT", "MESSAGE_UPSERT", "MESSAGES_SET":
		return normalizeMessageUpsert(ev, env.Data)
	case "MESSAGES_UPDATE", "MESSAGE_UPDATE":
		return normalizeMessageUpdate(ev, env.Data)
	case "SEND_MESSAGE", "MESSAGES_UPDATE_SEND":
		ev.EventType = whatsapp.EventMessageSent
		return normalizeMessageUpsert(ev, env.Data)
	case "CONNECTION_UPDATE", "CONNECTION_STATE":
		return normalizeConnection(ev, env.Data)
	default:
		ev.EventType = whatsapp.EventUnsupported
		return ev, nil
	}
}

type msgData struct {
	Key struct {
		RemoteJid string `json:"remoteJid"`
		FromMe    bool   `json:"fromMe"`
		ID        string `json:"id"`
	} `json:"key"`
	PushName         string         `json:"pushName"`
	MessageType      string         `json:"messageType"`
	Message          map[string]any `json:"message"`
	MessageTimestamp any            `json:"messageTimestamp"`
	Status           string         `json:"status"`
}

func normalizeMessageUpsert(ev whatsapp.ChannelEvent, data json.RawMessage) (whatsapp.ChannelEvent, error) {
	var m msgData
	if len(data) > 0 {
		_ = json.Unmarshal(data, &m)
		// Evolution sometimes wraps as { messages: [...] } or data is the message.
		if m.Key.ID == "" {
			var wrap struct {
				Messages []msgData `json:"messages"`
			}
			if json.Unmarshal(data, &wrap) == nil && len(wrap.Messages) > 0 {
				m = wrap.Messages[0]
			}
		}
	}
	if ev.EventType == "" {
		if m.Key.FromMe {
			ev.EventType = whatsapp.EventMessageSent
		} else {
			ev.EventType = whatsapp.EventMessageReceived
		}
	}
	ev.ExternalMessageID = m.Key.ID
	ev.ExternalEventID = m.Key.ID + ":" + ev.EventType
	phone := jidToE164(m.Key.RemoteJid)
	if m.Key.FromMe {
		ev.ToE164 = phone
	} else {
		ev.FromE164 = phone
	}
	ev.ExternalThreadID = phone
	if ts := parseTS(m.MessageTimestamp); !ts.IsZero() {
		ev.OccurredAt = ts
	}
	text := extractText(m.Message)
	if text != "" {
		ev.Content = whatsapp.Content{Type: whatsapp.ContentText, Text: text}
	}
	return ev, nil
}

func normalizeMessageUpdate(ev whatsapp.ChannelEvent, data json.RawMessage) (whatsapp.ChannelEvent, error) {
	var m msgData
	_ = json.Unmarshal(data, &m)
	status := strings.ToUpper(m.Status)
	// Also try nested keyStatus
	var alt struct {
		Key struct {
			ID        string `json:"id"`
			RemoteJid string `json:"remoteJid"`
		} `json:"keyId"`
		Status    string `json:"status"`
		MessageID string `json:"messageId"`
		RemoteJid string `json:"remoteJid"`
	}
	_ = json.Unmarshal(data, &alt)
	if m.Key.ID == "" {
		m.Key.ID = alt.MessageID
		if m.Key.ID == "" {
			m.Key.ID = alt.Key.ID
		}
	}
	if status == "" {
		status = strings.ToUpper(alt.Status)
	}
	switch status {
	case "DELIVERY_ACK", "DELIVERED", "SERVER_ACK":
		ev.EventType = whatsapp.EventMessageDelivered
	case "READ", "PLAYED":
		ev.EventType = whatsapp.EventMessageRead
	case "ERROR", "FAILED":
		ev.EventType = whatsapp.EventMessageFailed
	default:
		ev.EventType = whatsapp.EventMessageSent
	}
	ev.ExternalMessageID = m.Key.ID
	ev.ExternalEventID = m.Key.ID + ":" + ev.EventType + ":" + status
	phone := jidToE164(m.Key.RemoteJid)
	if phone == "" {
		phone = jidToE164(alt.RemoteJid)
	}
	ev.ToE164 = phone
	ev.ExternalThreadID = phone
	return ev, nil
}

func normalizeConnection(ev whatsapp.ChannelEvent, data json.RawMessage) (whatsapp.ChannelEvent, error) {
	ev.EventType = whatsapp.EventConnectionState
	var wrap struct {
		State    string `json:"state"`
		Instance struct {
			State    string `json:"state"`
			Instance string `json:"instanceName"`
		} `json:"instance"`
	}
	_ = json.Unmarshal(data, &wrap)
	ev.ConnectionState = wrap.State
	if ev.ConnectionState == "" {
		ev.ConnectionState = wrap.Instance.State
	}
	if wrap.Instance.Instance != "" {
		ev.Instance = wrap.Instance.Instance
	}
	ev.ExternalEventID = "connection:" + ev.Instance + ":" + ev.ConnectionState + ":" + ev.OccurredAt.Format(time.RFC3339)
	return ev, nil
}

func jidToE164(jid string) string {
	// "5548999999999@s.whatsapp.net" or "5548999999999:xx@s.whatsapp.net"
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return ""
	}
	at := strings.Index(jid, "@")
	if at > 0 {
		jid = jid[:at]
	}
	if colon := strings.Index(jid, ":"); colon > 0 {
		jid = jid[:colon]
	}
	digits := whatsapp.DigitsOnly(jid)
	if digits == "" {
		return ""
	}
	return "+" + digits
}

func extractText(msg map[string]any) string {
	if msg == nil {
		return ""
	}
	if c, ok := msg["conversation"].(string); ok {
		return c
	}
	if ext, ok := msg["extendedTextMessage"].(map[string]any); ok {
		if t, ok := ext["text"].(string); ok {
			return t
		}
	}
	return ""
}

func parseTS(v any) time.Time {
	switch t := v.(type) {
	case float64:
		if t > 1e12 {
			return time.UnixMilli(int64(t)).UTC()
		}
		return time.Unix(int64(t), 0).UTC()
	case int64:
		return time.Unix(t, 0).UTC()
	case json.Number:
		n, _ := t.Int64()
		return time.Unix(n, 0).UTC()
	case string:
		if n, err := time.Parse(time.RFC3339, t); err == nil {
			return n.UTC()
		}
	}
	return time.Time{}
}
