package jobs

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) runArrivalOutbox(ctx context.Context) {
	if s.ArrivalOutbox == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var failedSince, lastAlert time.Time
	for {
		pass, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.ArrivalOutbox.DeliverArrivals(pass, func(ctx context.Context, kind models.JobEventType, body any) error {
			return s.deliverArrivalEvent(ctx, kind, body)
		})
		if err != nil {
			log.Warn().Msg("sync arrival delivery deferred")
		}
		s.observeArrivalBacklog(pass, err, time.Now(), &failedSince, &lastAlert)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *JobsService) observeArrivalBacklog(ctx context.Context, deliveryErr error, now time.Time, failedSince, lastAlert *time.Time) {
	backlog := repository.ArrivalBacklog{}
	known := false
	if stats, ok := s.ArrivalOutbox.(repository.ArrivalBacklogRepository); ok {
		var err error
		backlog, err = stats.ArrivalBacklog(ctx)
		if err != nil {
			deliveryErr = err
		} else {
			known = true
		}
	}
	if deliveryErr != nil {
		if failedSince.IsZero() {
			*failedSince = now
		}
	} else {
		*failedSince = time.Time{}
	}
	age := time.Duration(0)
	if backlog.Oldest != nil {
		age = max(0, now.Sub(*backlog.Oldest))
	}
	if age < 5*time.Minute && (failedSince.IsZero() || now.Sub(*failedSince) < time.Minute) {
		return
	}
	if !lastAlert.IsZero() && now.Sub(*lastAlert) < 15*time.Minute {
		return
	}
	*lastAlert = now
	entry := log.Warn().Bool("backlog_known", known)
	if known {
		entry.Int("pending", backlog.Pending).Int64("oldest_age_seconds", int64(age.Seconds()))
	}
	entry.Msg("sync arrival queue needs attention")
	if s.OpsNotifier == nil {
		return
	}
	if s.Cache != nil {
		ok, err := s.Cache.SetNX(ctx, "sync:arrival:opsnotify", "1", 15*time.Minute).Result()
		if err == nil && !ok {
			return
		}
	}
	fields := map[string]string{"Pending": "unavailable", "Oldest age seconds": "unavailable"}
	if known {
		fields["Pending"] = strconv.Itoa(backlog.Pending)
		fields["Oldest age seconds"] = strconv.FormatInt(int64(age.Seconds()), 10)
	}
	s.OpsNotifier.NotifyOperator("sync.arrival.backlog", "Mailbox arrival delivery delayed",
		"Stored arrivals are waiting for delivery, or delivery checks are failing. Check the consumer and database before assuming sync is current.",
		fields)
}

func (s *JobsService) deliverArrivalEvent(ctx context.Context, kind models.JobEventType, body any) error {
	if _, ok := s.eventHandlers[kind]; !ok {
		return fmt.Errorf("durable arrival handler unavailable: %s", kind)
	}
	return s.HandleEvent(ctx, &models.JobEvent{Type: kind, Body: body})
}
