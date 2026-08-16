package plathealth

import (
	"context"
	"errors"
	"sync"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
)

// MemoryBus is an in-process EventBus with DeliverAll semantics. Tests and
// fixture-only probe runs use it so they do not need NATS.
type MemoryBus struct {
	mu      sync.Mutex
	buf     [][]byte
	waiters []chan []byte
	FailPub bool
	Drop    bool
}

// NewMemoryBus returns an empty in-process bus.
func NewMemoryBus() *MemoryBus { return &MemoryBus{} }

func (m *MemoryBus) Publish(_ context.Context, _, _ string, payload []byte) error {
	if m.FailPub {
		return errors.New("publish failed")
	}
	if m.Drop {
		return nil
	}
	cp := append([]byte(nil), payload...)
	m.mu.Lock()
	m.buf = append(m.buf, cp)
	waiters := append([]chan []byte(nil), m.waiters...)
	m.mu.Unlock()
	for _, w := range waiters {
		select {
		case w <- cp:
		default:
		}
	}
	return nil
}

func (m *MemoryBus) Subscribe(ctx context.Context, _ []string, _ string, handler eventbus.Handler) error {
	ch := make(chan []byte, 16)
	m.mu.Lock()
	for _, p := range m.buf {
		ch <- append([]byte(nil), p...)
	}
	m.waiters = append(m.waiters, ch)
	m.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p := <-ch:
			if err := handler(ctx, eventbus.Message{Topic: ProbeTopic, Payload: p}); err != nil {
				return err
			}
		}
	}
}

func (m *MemoryBus) Close() error { return nil }
func (m *MemoryBus) Name() string { return "memory" }
