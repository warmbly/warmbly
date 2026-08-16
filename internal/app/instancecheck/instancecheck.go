// Package instancecheck runs the operator-facing setup and health checks.
//
// A check answers one question an operator would otherwise have to answer by
// reading source: is this instance misconfigured, and what is the exact fix.
// Every message names the variable and the command, because a health page that
// only says "something is wrong" costs more time than it saves.
package instancecheck

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/app/instanceconfig"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/notify"
)

// perCheckTimeout bounds each check so one dead dependency cannot stall the
// whole page. Sized for the slowest check: two probe URLs, two attempts each.
const perCheckTimeout = 8 * time.Second

// Severity levels. Only non-ok findings are returned, so there is no "ok".
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Categories the admin panel groups findings under.
const (
	CategorySecurity = "security"
	CategoryURLs     = "urls"
	CategoryMail     = "mail"
	CategoryAccess   = "access"
	CategoryWorkers  = "workers"
	CategoryData     = "data"
)

// Finding is one thing that is wrong, plus how to fix it.
type Finding struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	Docs     string   `json:"docs"`
	// Target names the subject of a per-record finding (a mailbox address, a
	// worker name). Empty when the finding is instance-wide.
	Target string `json:"target"`
}

// Summary counts findings by severity.
type Summary struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

// Input carries the per-request facts a check cannot derive from the process.
type Input struct {
	// Host is the Host header the operator reached this backend on.
	Host string
	// Origin is the browser origin of the calling panel, when it sent one.
	Origin string
	// Forwarded reports whether the request carried X-Forwarded-For.
	Forwarded bool
}

// Deps are the process handles the checks read. Every field is optional: a
// check whose input is missing is skipped silently, never reported as failing.
type Deps struct {
	Runtime   *instanceconfig.Runtime
	Transport *notify.Transport
	Policy    *config.AuthPolicy
	DB        *pgxpool.Pool
	Cache     *cache.Cache
}

type check struct {
	id  string
	run func(ctx context.Context, d Deps, in Input) *Finding
}

// Registry holds the check set. Build it once at boot and reuse it.
type Registry struct {
	deps   Deps
	checks []check
}

// New builds the phase 1 registry.
func New(deps Deps) *Registry {
	r := &Registry{deps: deps}
	r.checks = append(r.checks, securityChecks()...)
	r.checks = append(r.checks, urlChecks()...)
	r.checks = append(r.checks, mailChecks()...)
	r.checks = append(r.checks, accessChecks()...)
	r.checks = append(r.checks, infraChecks()...)
	return r
}

// Run executes every check in parallel and returns the non-ok findings, most
// severe first, with ties broken by registration order.
func (r *Registry) Run(ctx context.Context, in Input) ([]Finding, Summary) {
	found := make([]*Finding, len(r.checks))

	var wg sync.WaitGroup
	for i, c := range r.checks {
		wg.Add(1)
		go func(i int, c check) {
			defer wg.Done()
			// A panicking check must degrade to a skip, never to a 500 on the
			// page an operator opens because something is already broken.
			defer func() { _ = recover() }()

			cctx, cancel := context.WithTimeout(ctx, perCheckTimeout)
			defer cancel()
			found[i] = c.run(cctx, r.deps, in)
		}(i, c)
	}
	wg.Wait()

	out := make([]Finding, 0, len(found))
	var summary Summary
	for i, f := range found {
		if f == nil {
			continue
		}
		f.ID = r.checks[i].id
		switch f.Severity {
		case SeverityError:
			summary.Error++
		case SeverityWarning:
			summary.Warning++
		default:
			summary.Info++
		}
		out = append(out, *f)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return severityRank(out[i].Severity) > severityRank(out[j].Severity)
	})
	return out, summary
}

func severityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 3
	case SeverityWarning:
		return 2
	default:
		return 1
	}
}
