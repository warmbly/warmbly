package eventbus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// natsAckGrace keeps the ack wait past the handler budget, so a message is not
// handed out again while its handler can still be running.
var natsAckGrace = 5 * time.Second

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

	// Held rather than passed: ensureStream retries after a failure, and the
	// retry callers have no config to hand it.
	maxAge     time.Duration
	maxBytes   int64
	replicas   int
	discardNew bool

	mu          sync.Mutex
	subscribers []jetstream.ConsumeContext
	streamMu    sync.Mutex
	streamReady bool
	closed      bool
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

	// MaxBytes caps the stream on disk. Zero leaves it unbounded, which a
	// managed account may refuse: Synadia's "Max Bytes Required" rejects any
	// stream created without one.
	MaxBytes   int64
	Replicas   int
	DiscardNew bool

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
	if cfg.Replicas < 0 || cfg.Replicas > 5 {
		return nil, errors.New("eventbus nats: replicas must be between 1 and 5, or zero to preserve the existing setting")
	}
	if cfg.StreamName == "" {
		cfg.StreamName = "warmbly"
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = "warmbly"
	}

	opts := append([]nats.Option{
		nats.Name("warmbly-eventbus"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}, cfg.Options...)

	// A JWT credential cannot travel in the URL, so it is resolved separately.
	creds, err := credsOption()
	if err != nil {
		return nil, err
	}
	if creds != nil {
		opts = append(opts, creds)
	}
	tlsOption, err := natsTLSOption()
	if err != nil {
		return nil, err
	}
	if tlsOption != nil {
		opts = append(opts, tlsOption)
	}

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

		maxAge:     cfg.MaxAge,
		maxBytes:   cfg.MaxBytes,
		replicas:   cfg.Replicas,
		discardNew: cfg.DiscardNew,
	}
	// Eagerly ensure the stream so misconfiguration surfaces at boot rather
	// than on the first publish. Failures are non-fatal here — the lazy
	// retry inside Publish/Subscribe will surface them to callers.
	if err := b.ensureStream(context.Background()); err != nil {
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

func (b *NATSBus) ensureStream(ctx context.Context) error {
	b.streamMu.Lock()
	defer b.streamMu.Unlock()
	if b.streamReady {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	filter := b.prefix + ".>"
	config := jetstream.StreamConfig{
		Name:      b.stream,
		Subjects:  []string{filter},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		MaxAge:    b.maxAge,
		MaxBytes:  maxBytesOrUnlimited(b.maxBytes),
		Discard:   jetstream.DiscardOld,
	}
	if config.MaxAge == 0 {
		config.MaxAge = 7 * 24 * time.Hour
	}
	stream, err := b.js.Stream(cctx, b.stream)
	var unchanged bool
	if err == nil {
		info, infoErr := stream.Info(cctx)
		if infoErr != nil {
			return fmt.Errorf("eventbus nats: read stream configuration: %w", infoErr)
		}
		config = info.Config
		unchanged = (b.maxAge == 0 || config.MaxAge == b.maxAge) &&
			(b.maxBytes == 0 || config.MaxBytes == b.maxBytes) &&
			(b.replicas == 0 || config.Replicas == b.replicas) &&
			(!b.discardNew || config.Discard == jetstream.DiscardNew)
		if !slices.Contains(config.Subjects, filter) {
			return fmt.Errorf("eventbus nats: stream %q does not cover configured subject prefix", b.stream)
		}
		if b.maxAge != 0 {
			config.MaxAge = b.maxAge
		}
		if b.maxBytes != 0 {
			config.MaxBytes = b.maxBytes
		}
	} else if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("eventbus nats: lookup stream: %w", err)
	}
	if b.replicas != 0 {
		config.Replicas = b.replicas
	}
	if b.discardNew {
		config.Discard = jetstream.DiscardNew
	}
	if unchanged {
		b.streamReady = true
		return nil
	}
	_, err = b.js.CreateOrUpdateStream(cctx, config)
	if err != nil {
		return fmt.Errorf("eventbus nats: ensure stream %q: %w%s", b.stream, err, maxBytesHint(err, b.maxBytes))
	}
	b.streamReady = true
	return nil
}

func (b *NATSBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBusClosed
	}
	b.mu.Unlock()

	if err := b.ensureStream(ctx); err != nil {
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
	return b.SubscribeKeyed(ctx, topics, group, 1, nil, handler)
}

// SubscribeKeyed hands messages with the same resolved key to one ordered lane
// and runs independent lanes concurrently, as the Kafka backend does.
func (b *NATSBus) SubscribeKeyed(ctx context.Context, topics []string, group string, lanes int, key KeyResolver, handler Handler) error {
	if len(topics) == 0 {
		return errors.New("eventbus nats: at least one topic required")
	}
	if group == "" {
		return errors.New("eventbus nats: consumer group required")
	}
	if handler == nil {
		return errors.New("eventbus nats: handler required")
	}
	if lanes < 1 || lanes > 64 || (lanes > 1 && key == nil) {
		return errors.New("eventbus nats: keyed subscription requires 1-64 lanes and a key resolver")
	}
	if err := b.ensureStream(ctx); err != nil {
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

	budget := subscriptionTimeout(ctx)
	ackWait := budget + natsAckGrace
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:        b.durable(group, topics),
		AckPolicy:      jetstream.AckExplicitPolicy,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		FilterSubjects: subjects,
		MaxDeliver:     10,
		AckWait:        ackWait,
	})
	if err != nil {
		return fmt.Errorf("eventbus nats: create consumer: %w", err)
	}

	deliver := func(m jetstream.Msg) {
		// A message already being handled finishes after a shutdown signal; only reading the next one stops.
		hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
		defer cancel()
		if err := invokeHandler(hctx, handler, b.envelope(m)); err != nil {
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
	}

	var pool *natsLanes
	var cc jetstream.ConsumeContext
	if lanes > 1 {
		pool = newNATSLanes(lanes, ackWait, deliver)
		cc = pool.start(ctx, cons, func(m jetstream.Msg) (string, error) {
			return resolveKey(ctx, key, b.envelope(m))
		})
	} else if cc, err = cons.Consume(deliver); err != nil {
		return fmt.Errorf("eventbus nats: consume: %w", err)
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		if pool != nil {
			pool.halt()
		}
		cc.Stop()
		<-cc.Closed()
		if pool != nil {
			pool.stop()
		}
		return ErrBusClosed
	}
	b.subscribers = append(b.subscribers, cc)
	b.mu.Unlock()

	// Block until ctx is cancelled, mirroring KafkaBus.Subscribe semantics.
	<-ctx.Done()
	if pool != nil {
		pool.halt()
	}
	cc.Stop()
	// Closed fires once the callback in progress has returned, so shutdown waits for it.
	<-cc.Closed()
	if pool != nil {
		pool.stop()
	}
	return ctx.Err()
}

// envelope reports a JetStream message in the bus-neutral form handlers read.
func (b *NATSBus) envelope(m jetstream.Msg) Message {
	key := m.Headers().Get("Warmbly-Key")
	if key == "" {
		key = m.Headers().Get(nats.MsgIdHdr)
	}
	attempt := 1
	if meta, err := m.Metadata(); err == nil && meta.NumDelivered > 0 {
		attempt = int(meta.NumDelivered)
	}
	return Message{
		// Handlers see the topic they published to, without the stream prefix.
		Topic:      strings.TrimPrefix(m.Subject(), b.prefix+"."),
		Key:        key,
		Payload:    m.Data(),
		Attempt:    attempt,
		Redelivers: true,
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
		<-s.Closed()
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
var (
	_ EventBus        = (*NATSBus)(nil)
	_ KeyedSubscriber = (*NATSBus)(nil)
)

// maxBytesOrUnlimited maps an unset ceiling onto JetStream's own spelling for
// it. Zero in a StreamConfig means zero bytes, not unlimited.
func maxBytesOrUnlimited(v int64) int64 {
	if v <= 0 {
		return -1
	}
	return v
}

// maxBytesHint turns the server's refusal into the thing to do about it. The
// bare error names a policy the operator has probably never heard of.
func maxBytesHint(err error, maxBytes int64) string {
	if maxBytes > 0 || err == nil {
		return ""
	}
	if !strings.Contains(strings.ToLower(err.Error()), "max bytes") {
		return ""
	}
	return " (this account requires every stream to declare a size; set NATS_MAX_BYTES, e.g. NATS_MAX_BYTES=1GiB)"
}
