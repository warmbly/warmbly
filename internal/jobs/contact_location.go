package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// contactLocationMaxPasses bounds one run; the next tick picks up the rest.
	contactLocationMaxPasses = 100
	contactLocationLease     = "lock:contactlocation"
	contactLocationLeaseTTL  = 5 * time.Minute
	contactLocationRunBudget = contactLocationLeaseTTL - time.Minute
)

var releaseContactLocationLease = redis.NewScript(`if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`)

// ContactLocationStore is the slice of the contact repository the sweep uses.
type ContactLocationStore interface {
	ListLocationPending(ctx context.Context, limit int) ([]repository.ContactLocationPending, error)
	SetContactLocations(ctx context.Context, results []repository.ContactLocationResult) ([]uuid.UUID, error)
}

// ContactLocationSweep works out each contact's timezone and country from
// what its record carries (geo.Resolve) and stores them for segments and
// recipient-local sending. It reads only the contacts whose inputs changed,
// whichever path wrote them.
type ContactLocationSweep struct {
	contacts ContactLocationStore
	cache    *cache.Cache
	reloader ContactsReloader
}

func NewContactLocationSweep(contacts ContactLocationStore, c *cache.Cache, reloader ContactsReloader) *ContactLocationSweep {
	return &ContactLocationSweep{contacts: contacts, cache: c, reloader: reloader}
}

func (j *ContactLocationSweep) Run(ctx context.Context) error {
	// One replica sweeps at a time; a Redis outage fails open.
	if j.cache != nil {
		token := uuid.NewString()
		if got, err := j.cache.SetNX(ctx, contactLocationLease, token, contactLocationLeaseTTL).Result(); err == nil {
			if !got {
				return nil
			}
			defer releaseContactLocationLease.Run(context.WithoutCancel(ctx), j.cache, []string{contactLocationLease}, token)
		}
	}
	deadline := time.Now().Add(contactLocationRunBudget)
	changed := map[uuid.UUID]bool{}
	defer func() {
		if j.reloader == nil {
			return
		}
		for org := range changed {
			j.reloader.PublishOrgContactsReload(context.WithoutCancel(ctx), org.String(), "contacts:location")
		}
	}()
	for range contactLocationMaxPasses {
		pending, err := j.contacts.ListLocationPending(ctx, config.ContactLocationBatchSize)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		orgs, err := j.contacts.SetContactLocations(ctx, ResolveContactLocations(pending))
		if err != nil {
			return err
		}
		for _, org := range orgs {
			changed[org] = true
		}
		if len(pending) < config.ContactLocationBatchSize || time.Now().After(deadline) || ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// ResolveContactLocations answers one batch. A contact nothing places is
// still answered, with an empty location, so it leaves the pending set.
func ResolveContactLocations(pending []repository.ContactLocationPending) []repository.ContactLocationResult {
	out := make([]repository.ContactLocationResult, len(pending))
	for i, p := range pending {
		loc := geo.Resolve(geo.Input{Fields: p.CustomFields, Phone: p.Phone, Email: p.Email})
		out[i] = repository.ContactLocationResult{
			ID: p.ID, Fingerprint: p.Fingerprint,
			Timezone: loc.Timezone, Country: loc.Country, Source: string(loc.Source),
		}
	}
	return out
}

// Start runs the sweep on boot and then on the interval until ctx ends.
func (j *ContactLocationSweep) Start(ctx context.Context) {
	interval := time.Duration(config.ContactLocationIntervalSeconds) * time.Second
	jobrun.Loop(ctx, "contact_location", interval, true, j.Run)
}
