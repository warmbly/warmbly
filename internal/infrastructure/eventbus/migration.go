package eventbus

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"

	"golang.org/x/sync/errgroup"
)

// migrationBus publishes once and keeps the previous broker's consumer offsets advancing.
type migrationBus struct {
	primary EventBus
	other   EventBus
	hold    string
}

func (b *migrationBus) Name() string { return b.primary.Name() }

func (b *migrationBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	return b.primary.Publish(ctx, topic, key, payload)
}

func (b *migrationBus) Close() error {
	return errors.Join(b.primary.Close(), b.other.Close())
}

func (b *migrationBus) Diagnose(ctx context.Context, at time.Time, scopes []DiagnosticScope) models.MonitoringSource {
	bus := b.primary
	if bus.Name() == b.hold {
		bus = b.other
	}
	if diagnostics, ok := bus.(Diagnostics); ok {
		out := diagnostics.Diagnose(ctx, at, scopes)
		for i := range out.Metrics {
			out.Metrics[i].Note = "Migration observation from " + bus.Name() + ". " + out.Metrics[i].Note
		}
		return out
	}
	return models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: "unsupported"}
}

func (b *migrationBus) Subscribe(ctx context.Context, topics []string, group string, handler Handler) error {
	return b.subscribe(ctx, func(ctx context.Context, bus EventBus) error {
		return bus.Subscribe(ctx, topics, group, handler)
	})
}

func (b *migrationBus) SubscribeKeyed(ctx context.Context, topics []string, group string, lanes int, key KeyResolver, handler Handler) error {
	return b.subscribe(ctx, func(ctx context.Context, bus EventBus) error {
		return SubscribeKeyed(ctx, bus, topics, group, lanes, key, handler)
	})
}

func (b *migrationBus) subscribe(ctx context.Context, subscribe func(context.Context, EventBus) error) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, bus := range []EventBus{b.primary, b.other} {
		g.Go(func() error {
			if bus.Name() == b.hold {
				log.Info().Str("provider", b.hold).Msg("eventbus migration: consumption held until the old transport drains")
				<-ctx.Done()
				return ctx.Err()
			}
			err := subscribe(ctx, bus)
			if err == nil && ctx.Err() == nil {
				return errors.New("eventbus: migration subscription stopped unexpectedly")
			}
			return err
		})
	}
	return g.Wait()
}
