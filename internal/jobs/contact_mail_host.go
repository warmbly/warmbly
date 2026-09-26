package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// contactMailHostMaxPasses bounds one run; the next tick picks up the rest.
	contactMailHostMaxPasses = 50
	contactMailHostLease     = "lock:contactmailhost"
	contactMailHostLeaseTTL  = 10 * time.Minute
)

// ContactMailHostStore is the slice of the contact repository the sweep uses.
type ContactMailHostStore interface {
	ListMailHostPending(ctx context.Context, limit int) ([]repository.ContactMailHostPending, error)
	SetContactMailHosts(ctx context.Context, results []repository.ContactMailHostResult) ([]uuid.UUID, error)
}

// ContactsReloader tells an organization's members their contact lists changed.
type ContactsReloader interface {
	PublishOrgContactsReload(ctx context.Context, orgID, operationID string)
}

// ContactMailHostSweep works out who hosts each contact's inbox from the
// domain's DNS, one lookup per distinct domain, and stores it on the contact
// with the family ESP matching reads. Control plane only: it dials DNS for
// user-supplied domains.
type ContactMailHostSweep struct {
	contacts ContactMailHostStore
	cache    *cache.Cache
	resolver mailhost.RecipientResolver
	reloader ContactsReloader
}

func NewContactMailHostSweep(contacts ContactMailHostStore, c *cache.Cache, reloader ContactsReloader) *ContactMailHostSweep {
	return &ContactMailHostSweep{contacts: contacts, cache: c, reloader: reloader}
}

type cachedMailHost struct {
	Host string `json:"host"`
}

func (j *ContactMailHostSweep) Run(ctx context.Context) error {
	// One replica sweeps at a time; a Redis outage fails open.
	if j.cache != nil {
		if got, err := j.cache.SetNX(ctx, contactMailHostLease, "1", contactMailHostLeaseTTL).Result(); err == nil {
			if !got {
				return nil
			}
			defer j.cache.Del(context.WithoutCancel(ctx), contactMailHostLease)
		}
	}
	for range contactMailHostMaxPasses {
		pending, err := j.contacts.ListMailHostPending(ctx, config.ContactMailHostBatchSize)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		hosts := j.resolveDomains(ctx, pending)
		results := make([]repository.ContactMailHostResult, 0, len(pending))
		for _, p := range pending {
			r, ok := hosts[mailhost.NormalizeDomain(p.Email)]
			res := repository.ContactMailHostResult{ID: p.ID, Email: p.Email, Transient: !ok}
			if ok {
				res.MailHost, res.ESP = string(r), mailhost.ESPFamily(r)
			}
			results = append(results, res)
		}
		orgs, err := j.contacts.SetContactMailHosts(ctx, results)
		if err != nil {
			return err
		}
		if j.reloader != nil {
			for _, org := range orgs {
				j.reloader.PublishOrgContactsReload(ctx, org.String(), "contacts:mail_host")
			}
		}
		if len(pending) < config.ContactMailHostBatchSize || ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// resolveDomains answers each distinct domain once. A domain missing from the
// result failed transiently.
func (j *ContactMailHostSweep) resolveDomains(ctx context.Context, pending []repository.ContactMailHostPending) map[string]mailhost.Host {
	out := map[string]mailhost.Host{}
	var todo []string
	for _, p := range pending {
		d := mailhost.NormalizeDomain(p.Email)
		if _, seen := out[d]; seen {
			continue
		}
		out[d] = mailhost.Unknown
		if d == "" {
			continue
		}
		var hit cachedMailHost
		if j.cache != nil && j.cache.GetJSON(ctx, contactMailHostKey(d), &hit) == nil {
			out[d] = mailhost.Host(hit.Host)
			continue
		}
		todo = append(todo, d)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, config.ContactMailHostConcurrency)
	for _, d := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func(d string) {
			defer wg.Done()
			defer func() { <-sem }()
			h, ok := mailhost.Recipient(ctx, j.resolver, d)
			if ok && j.cache != nil {
				ttl := time.Duration(config.ContactMailHostCacheHours) * time.Hour
				if err := j.cache.SetJSON(ctx, contactMailHostKey(d), cachedMailHost{Host: string(h)}, ttl); err != nil {
					log.Debug().Err(err).Msg("contact mail host: cache write failed")
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if ok {
				out[d] = h
			} else {
				delete(out, d)
			}
		}(d)
	}
	wg.Wait()
	return out
}

func contactMailHostKey(domain string) string { return "contactmailhost:" + domain }

// Start runs the sweep on boot and then on the interval until ctx ends.
func (j *ContactMailHostSweep) Start(ctx context.Context) {
	interval := time.Duration(config.ContactMailHostIntervalSeconds) * time.Second
	jobrun.Loop(ctx, "contact_mail_host", interval, true, j.Run)
}
