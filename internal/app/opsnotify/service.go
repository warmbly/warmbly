package opsnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

// deliveryTimeout bounds one outbound call. Chat webhooks answer in
// milliseconds; anything slower is not worth holding a goroutine for.
const deliveryTimeout = 8 * time.Second

// maxInFlight bounds concurrent deliveries. Notify is called from request
// paths that are open to the internet (signup emits user.registered), and each
// delivery can hold a goroutine for the full timeout against a slow endpoint.
// A bounded pool means a hostile or dead chat server costs a fixed amount of
// this process instead of one goroutine per event.
const maxInFlight = 8

// Settings is the slice of the settings service this package needs.
type Settings interface {
	Get(ctx context.Context) instancesettings.Document
}

// Mailer is the narrow slice of notify.EmailNotificationService this package
// needs, declared locally so opsnotify does not depend on the mail package.
// `message` is HTML; the transports derive the text part themselves.
//
// Operator alerts deliberately bypass internal/app/notification: that path is
// per-user, tenant-scoped, digest-coalesced and daily-capped, so an alert sent
// through it could be delayed or silently dropped.
type Mailer interface {
	Send(ctx context.Context, to, cc, bcc []string, subject, message string) error
}

// Notifier is the emit surface. Call sites depend on this, never on the
// concrete service, so a nil notifier is a no-op rather than a panic.
type Notifier interface {
	// Notify delivers to every subscribed channel. It never blocks the caller
	// and never returns an error: an operator alert must not be able to fail
	// the request that produced it.
	Notify(event Event)
	// NotifyOperator is the plain-string form every emit site uses, so a
	// package can declare a one-method local interface for it and stay free of
	// any dependency on this one.
	NotifyOperator(key, title, summary string, fields map[string]string)
	// Deliver sends one event to one channel and reports the outcome. The
	// admin panel's "send test" uses it; nothing else should.
	Deliver(ctx context.Context, ch instancesettings.NotifyChannel, event Event) error
}

type service struct {
	settings Settings
	mailer   Mailer
	client   *http.Client
	// baseURL is the admin panel origin, used to build the Link on events.
	baseURL string
	// slots bounds concurrent deliveries; an event that cannot claim one is
	// dropped rather than queued. Operator alerts are best effort, and a
	// backlog that outlives the incident is worse than a missed line.
	slots chan struct{}
	// dropped counts events shed under load, so the condition is observable
	// instead of silent.
	dropped atomic.Uint64
}

// NewService builds the notifier. A nil settings service disables delivery.
func NewService(settings Settings, mailer Mailer, baseURL string) Notifier {
	return &service{
		settings: settings,
		mailer:   mailer,
		client:   safehttp.Client(deliveryTimeout),
		baseURL:  strings.TrimRight(baseURL, "/"),
		slots:    make(chan struct{}, maxInFlight),
	}
}

// Nop is the notifier used where none is configured.
type Nop struct{}

func (Nop) Notify(Event) {}
func (Nop) Deliver(context.Context, instancesettings.NotifyChannel, Event) error {
	return fmt.Errorf("notifications are not configured on this deployment")
}

func (s *service) Notify(event Event) {
	if s == nil || s.settings == nil {
		return
	}
	// Claim a slot before detaching. Non-blocking: the caller is on a request
	// path and must never wait on a chat server.
	select {
	case s.slots <- struct{}{}:
	default:
		if n := s.dropped.Add(1); n == 1 || n%100 == 0 {
			log.Warn().Uint64("dropped", n).Str("event", event.Key).
				Msg("operator notification dropped: delivery slots are full")
		}
		return
	}

	// Detached context: the caller's request may finish (or be cancelled)
	// before a chat server answers, and that must not drop the alert.
	go func() {
		defer func() { <-s.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), deliveryTimeout*2)
		defer cancel()

		subs := s.settings.Get(ctx).Notifications.Subscribers(event.Key)
		for _, ch := range subs {
			// Sequential: the list is bounded at MaxChannels and this runs off
			// the request path, so there is nothing to gain from more goroutines.
			_ = s.Deliver(ctx, ch, event)
		}
	}()
}

func (s *service) Deliver(ctx context.Context, ch instancesettings.NotifyChannel, event Event) error {
	switch ch.Type {
	case instancesettings.ChannelDiscord:
		return s.post(ctx, ch, discordPayload(event), nil)
	case instancesettings.ChannelSlack:
		return s.post(ctx, ch, slackPayload(event), nil)
	case instancesettings.ChannelWebhook:
		return s.postSigned(ctx, ch, event)
	case instancesettings.ChannelEmail:
		if s.mailer == nil {
			return fmt.Errorf("no mail transport is configured on this deployment")
		}
		return s.mailer.Send(ctx, []string{ch.Target}, nil, nil, emailSubject(event), emailBodyHTML(event))
	default:
		return fmt.Errorf("unknown channel type %q", ch.Type)
	}
}

func (s *service) post(ctx context.Context, ch instancesettings.NotifyChannel, payload any, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.send(ctx, ch.Target, body, headers)
}

// postSigned is the generic webhook: the event as JSON, signed with the same
// HMAC scheme customer webhooks use so an existing verifier works unchanged.
func (s *service) postSigned(ctx context.Context, ch instancesettings.NotifyChannel, event Event) error {
	payload := map[string]any{
		"event":     event.Key,
		"title":     event.Title,
		"summary":   event.Summary,
		"severity":  string(event.Severity),
		"fields":    event.Fields,
		"link":      event.Link,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	headers := map[string]string{}
	if ch.Secret != "" {
		now := time.Now()
		headers["X-Warmbly-Signature"] = webhook.FormatSignatureHeader(now, webhook.Sign(ch.Secret, now, body))
	}
	headers["X-Warmbly-Event"] = event.Key
	return s.send(ctx, ch.Target, body, headers)
}

func (s *service) send(ctx context.Context, url string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Warmbly-Ops-Notifier/1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Read and discard so the connection can be reused; cap it so a hostile
	// endpoint cannot stream us an unbounded body.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("endpoint returned %d", resp.StatusCode)
	}
	return nil
}

// WithLink returns a copy of the event pointing at an admin panel path.
func (s *service) WithLink(event Event, path string) Event {
	if s.baseURL != "" && path != "" {
		event.Link = s.baseURL + path
	}
	return event
}

// NotifyOperator is the emit surface every call site uses. It takes plain
// strings rather than an Event so a package can declare a one-method local
// interface and stay free of any dependency on this one.
//
// Fields are rendered in sorted key order: a map has no order, and an alert
// whose lines move between deliveries is hard to read.
func (s *service) NotifyOperator(key, title, summary string, fields map[string]string) {
	event := NewEvent(key, title, summary)
	if len(fields) > 0 {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.TrimSpace(fields[k]) == "" {
				continue
			}
			event.Fields = append(event.Fields, Field{Label: k, Value: fields[k]})
		}
	}
	s.Notify(event)
}

// NotifyOperator on the no-op notifier discards the alert.
func (Nop) NotifyOperator(string, string, string, map[string]string) {}
