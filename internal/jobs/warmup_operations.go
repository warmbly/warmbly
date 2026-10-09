package jobs

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/opsnotify"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

const warmupAlertCooldown = 6 * time.Hour

type warmupLoadingStore interface {
	ListMailboxLoadingIncidents(context.Context, time.Time) ([]repository.MailboxLoadingIncident, error)
}

type warmupDispatchStore interface {
	ListOverdueWarmupDispatches(context.Context, time.Time) ([]repository.MailboxLoadingIncident, error)
}

type WarmupOperationsJob struct {
	loading  warmupLoadingStore
	dispatch warmupDispatchStore
	notifier interface {
		NotifyOperator(string, string, string, map[string]string)
	}
	claim  func(context.Context, string) (bool, error)
	clock  func() time.Time
	report func(error, ...errs.Option)
}

func NewWarmupOperationsJob(loading warmupLoadingStore, dispatch warmupDispatchStore, c *cache.Cache, notifier interface {
	NotifyOperator(string, string, string, map[string]string)
}) *WarmupOperationsJob {
	return &WarmupOperationsJob{
		loading: loading, dispatch: dispatch, notifier: notifier, clock: time.Now, report: errs.CaptureException,
		claim: func(ctx context.Context, key string) (bool, error) {
			if c == nil {
				return false, errors.New("warmup alert cooldown unavailable")
			}
			claimed, err := c.SetNX(ctx, "warmup:opsnotify:"+key, "1", warmupAlertCooldown).Result()
			if err != nil {
				return false, errors.New("warmup alert cooldown unavailable")
			}
			return claimed, nil
		},
	}
}

func (j *WarmupOperationsJob) Start(ctx context.Context) {
	jobrun.Loop(ctx, "warmup_operations", 5*time.Minute, true, j.Run)
}

func (j *WarmupOperationsJob) Run(ctx context.Context) error {
	now := j.clock()
	loading, err := j.loading.ListMailboxLoadingIncidents(ctx, now)
	if err != nil {
		return errors.New("warmup loading monitor query failed")
	}
	overdue, err := j.dispatch.ListOverdueWarmupDispatches(ctx, now)
	if err != nil {
		return errors.New("warmup dispatch monitor query failed")
	}
	for _, v := range []struct {
		kind string
		rows []repository.MailboxLoadingIncident
	}{
		{"mailbox_loading", loading}, {"dispatch_overdue", overdue},
	} {
		if err := j.alert(ctx, v.kind, v.rows, now); err != nil {
			return err
		}
	}
	return nil
}

func (j *WarmupOperationsJob) alert(ctx context.Context, kind string, rows []repository.MailboxLoadingIncident, now time.Time) error {
	groups := make(map[string][]repository.MailboxLoadingIncident)
	for _, row := range rows {
		key := "unassigned"
		if row.WorkerID != nil {
			key = row.WorkerID.String()
		}
		groups[key] = append(groups[key], row)
	}
	if len(rows) >= 10 && len(groups) >= 2 {
		groups = map[string][]repository.MailboxLoadingIncident{"fleet": rows}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		claimed, err := j.claim(ctx, kind+":"+key)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		orgs, workers := make(map[uuid.UUID]bool), make(map[uuid.UUID]bool)
		oldest := now
		for _, row := range group {
			orgs[row.OrganizationID] = true
			if row.WorkerID != nil {
				workers[*row.WorkerID] = true
			}
			if row.FirstFailureAt.Before(oldest) {
				oldest = row.FirstFailureAt
			}
		}
		title, summary := "Persistent warmup worker-loading failures", "Recent worker-loading failures span at least an hour without a later confirmed warmup send. Check worker loading and reload delivery; this is not evidence of rejected credentials."
		event := opsnotify.EventWarmupLoading
		if kind == "dispatch_overdue" {
			title, summary = "Warmup task dispatch is overdue", "Active mailboxes have pending warmup tasks at least an hour past their intended schedule. Check dispatcher errors and retry/backoff. This does not establish a provider failure."
			event = opsnotify.EventWarmupDispatchOverdue
		}
		fields := map[string]string{"Scope": key, "Mailboxes": strconv.Itoa(len(group)), "Workspaces": strconv.Itoa(len(orgs)), "Workers": strconv.Itoa(len(workers)), "Oldest observed delay": now.Sub(oldest).Round(time.Minute).String()}
		if j.notifier != nil {
			j.notifier.NotifyOperator(event, title, summary, fields)
		}
		log.Error().Str("category", kind).Int("mailboxes", len(group)).Int("workers", len(workers)).Int("workspaces", len(orgs)).Msg(title)
		j.report(errors.New(title), errs.Tag("category", kind), errs.Extra("affected_mailboxes", len(group)), errs.Extra("affected_workers", len(workers)), errs.Extra("affected_workspaces", len(orgs)), errs.Extra("oldest_delay_minutes", int(now.Sub(oldest).Minutes())))
	}
	return nil
}
