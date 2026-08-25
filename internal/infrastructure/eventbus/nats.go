package eventbus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// NATSBus is the EventBus backed by NATS JetStream. JetStream (not core NATS)
// is required because the rest of the system assumes durable, replayable
// delivery: a worker that goes offline must resume from its last ack.
//
// Subjects are derived from Kafka-style topics via Subject() — see bus.go.
// All managed subjects live under a single stream (default name "warmbly")
// whose subject filter is "<prefix>.>". The prefix is configurable, defaults
// to "warmbly", and is prepended to every published topic; this keeps the
// stream's subject space contained even when callers use generic topic names
// like "w.<uuid>" or "jobs.worker-events".
//
// Consumer groups map to JetStream durable consumer names. Each Subscribe
// call creates (or reuses) one durable per group + topic combination and
// pulls messages with explicit ack.
type NATSBus struct {
	nc     *nats.Conn
	js     jetstream.JetStream
	stream string
	prefix string

	mu           sync.Mutex
	subscribers  []jetstream.ConsumeContext
	streamEnsure sync.Once
	streamErr    error
	closed       bool
}

// NATSConfig builds a NATSBus.
type NATSConfig struct {
	// URL is a nats:// (or tls://) endpoint. Comma-separated for multiple
	// servers, matching nats.go's expectations.
	URL string

	// StreamName is the JetStream stream that hosts all managed subjects.
	// Default: "warmbly".
	StreamName string

	// SubjectPrefix is prepended to every topic. Default: "warmbly". The
	// stream's subject filter becomes "<SubjectPrefix>.>".
	SubjectPrefix string

	// MaxAge bounds how long unacknowledged messages are retained. Zero means
	// "use the stream's existing setting or 7 days for new streams".
	MaxAge time.Duration

	// Options passed to nats.Connect (auth, TLS, etc).
	Options []nats.Option
}

// NewNATS dials NATS and prepares the JetStream context. The stream is
// created lazily on the first Publish or Subscribe to keep boot side-effects
// scoped to the operations that need them.
func NewNATS(cfg NATSConfig) (*NATSBus, error) {
	if cfg.URL == "" {
		return nil, errors.New("eventbus nats: URL required")
	}
	if cfg.StreamName == "" {
		cfg.StreamName = "warmbly"
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = "warmbly"
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 7 * 24 * time.Hour
	}

	opts := append([]nats.Option{
		nats.Name("warmbly-eventbus"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}, cfg.Options...)

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("eventbus nats: connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("eventbus nats: jetstream: %w", err)
	}

	b := &NATSBus{
		nc:     nc,
		js:     js,
		stream: cfg.StreamName,
		prefix: cfg.SubjectPrefix,
	}
	// Eagerly ensure the stream so misconfiguration surfaces at boot rather
	// than on the first publish. Failures are non-fatal here — the lazy
	// retry inside Publish/Subscribe will surface them to callers.
	if err := b.ensureStream(context.Background(), cfg.MaxAge); err != nil {
		log.Warn().Err(err).Msg("eventbus nats: deferred stream setup")
	}
	return b, nil
}

func (b *NATSBus) Name() string { return "nats" }

// subject prepends the configured prefix to the (translated) topic. Callers
// pass Kafka-style topics ("w:<uuid>") and we map to "<prefix>.w.<uuid>".
func (b *NATSBus) subject(topic string) string {
	return b.prefix + "." + Subject(topic)
}

// durable computes a JetStream durable consumer name. JetStream names allow
// alnum, "-" and "_" only, so we substitute anything else.
func (b *NATSBus) durable(group string, topics []string) string {
	parts := []string{group}
	for _, t := range topics {
		parts = append(parts, t)
	}
	raw := strings.Join(parts, "_")
	var sb strings.Builder
	sb.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

func (b *NATSBus) ensureStream(ctx context.Context, maxAge time.Duration) error {
	b.streamEnsure.Do(func() {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		filter := b.prefix + ".>"
		_, err := b.js.CreateOrUpdateStream(cctx, jetstream.StreamConfig{
			Name:      b.stream,
			Subjects:  []string{filter},
			Retention: jetstream.LimitsPolicy,
			Storage:   jetstream.FileStorage,
			MaxAge:    maxAge,
			Discard:   jetstream.DiscardOld,
		})
		if err != nil {
			b.streamErr = fmt.Errorf("eventbus nats: ensure stream %q: %w", b.stream, err)
			b.streamEnsure = sync.Once{} // allow retry on next call
		}
	})
	return b.streamErr
}

func (b *NATSBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("eventbus nats: bus closed")
	}
	b.mu.Unlock()

	if err := b.ensureStream(ctx, 0); err != nil {
		return err
	}

	msg := &nats.Msg{
		Subject: b.subject(topic),
		Data:    payload,
		Header:  nats.Header{},
	}
	if key != "" {
		// Carried as a plain header, never as Nats-Msg-Id: the key is a
		// partition/affinity hint (one mailbox, one org), not a unique event
		// id, so feeding it to JetStream's dedup window would silently drop
		// every event after the first for that key within the window.
		msg.Header.Set("Warmbly-Key", key)
	}

	_, err := b.js.PublishMsg(ctx, msg)
	if err != nil {
		return fmt.Errorf("eventbus nats: publish %s: %w", topic, err)
	}
	return nil
}

func (b *NATSBus) Subscribe(ctx context.Context, topics []string, group string, handler Handler) error {
	if len(topics) == 0 {
		return errors.New("eventbus nats: at least one topic required")
	}
	if group == "" {
		return errors.New("eventbus nats: consumer group required")
	}
	if handler == nil {
		return errors.New("eventbus nats: handler required")
	}
	if err := b.ensureStream(ctx, 0); err != nil {
		return err
	}

	subjects := make([]string, len(topics))
	for i, t := range topics {
		subjects[i] = b.subject(t)
	}

	stream, err := b.js.Stream(ctx, b.stream)
	if err != nil {
		return fmt.Errorf("eventbus nats: lookup stream: %w", err)
	}

	consumerConfig := jetstream.ConsumerConfig{
		Durable:        b.durable(group, topics),
		AckPolicy:      jetstream.AckExplicitPolicy,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		FilterSubjects: subjects,
		MaxDeliver:     10,
		AckWait:        handlerTimeout() + 5*time.Second,
	}
	if strings.HasPrefix(group, TransientConsumerPrefix) {
		consumerConfig.DeliverPolicy = jetstream.DeliverLastPolicy
		consumerConfig.InactiveThreshold = time.Minute
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, consumerConfig)
	if err != nil {
		return fmt.Errorf("eventbus nats: create consumer: %w", err)
	}

	cc, err := cons.Consume(func(m jetstream.Msg) {
		hctx, cancel := context.WithTimeout(ctx, handlerTimeout())
		defer cancel()
		key := m.Headers().Get("Warmbly-Key")
		if key == "" {
			key = m.Headers().Get(nats.MsgIdHdr)
		}
		// Strip the prefix when reporting topic back to handlers so they see
		// the same string they published with. Note: NATS subjects use "."
		// while Kafka topics use ":" — handlers may receive either form
		// depending on which backend is wired in. New code should not rely
		// on the exact separator and should use Subject() if it needs to
		// compare against a Kafka-style topic name.
		topic := strings.TrimPrefix(m.Subject(), b.prefix+".")
		attempt := 1
		if meta, merr := m.Metadata(); merr == nil && meta.NumDelivered > 0 {
			attempt = int(meta.NumDelivered)
		}
		if err := invokeHandler(hctx, handler, Message{
			Topic:      topic,
			Key:        key,
			Payload:    m.Data(),
			Attempt:    attempt,
			Redelivers: true,
		}); err != nil {
			log.Error().Err(err).Str("subject", m.Subject()).Msg("eventbus nats handler error")
			// Nak with a short delay so transient errors don't hot-loop.
			if nakErr := m.NakWithDelay(time.Second); nakErr != nil {
				log.Warn().Err(nakErr).Msg("eventbus nats nak failed")
			}
			return
		}
		if ackErr := m.Ack(); ackErr != nil {
			log.Warn().Err(ackErr).Msg("eventbus nats ack failed")
		}
	})
	if err != nil {
		return fmt.Errorf("eventbus nats: consume: %w", err)
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cc.Stop()
		return errors.New("eventbus nats: bus closed")
	}
	b.subscribers = append(b.subscribers, cc)
	b.mu.Unlock()

	// Block until ctx is cancelled, mirroring KafkaBus.Subscribe semantics.
	<-ctx.Done()
	cc.Stop()
	b.removeSubscriber(cc)
	return ctx.Err()
}

func (b *NATSBus) removeSubscriber(target jetstream.ConsumeContext) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, subscriber := range b.subscribers {
		if subscriber != target {
			continue
		}
		copy(b.subscribers[i:], b.subscribers[i+1:])
		b.subscribers[len(b.subscribers)-1] = nil
		b.subscribers = b.subscribers[:len(b.subscribers)-1]
		return
	}
}

func (b *NATSBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := b.subscribers
	b.subscribers = nil
	b.mu.Unlock()

	for _, s := range subs {
		s.Stop()
	}
	if b.nc != nil {
		// Drain rather than hard-close to flush in-flight publishes.
		if err := b.nc.Drain(); err != nil {
			b.nc.Close()
			return err
		}
	}
	return nil
}

// natsURLFromEnv reads NATS_URL with a sensible default so the factory and
// tests can share the same source of truth.
func natsURLFromEnv() string {
	if v := os.Getenv("NATS_URL"); v != "" {
		return v
	}
	return nats.DefaultURL
}

// Compile-time interface check.
var _ EventBus = (*NATSBus)(nil)
