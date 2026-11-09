package leadfinder

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

const (
	enrichChunk       = 24
	enrichParallel    = 6
	enrichVerifyMax   = 3
	enrichRunTimeout  = 45 * time.Minute
	enrichLeadTimeout = 50 * time.Second
	// enrichLeadSlowTimeout leaves room for a reader service and web searches.
	enrichLeadSlowTimeout = 2 * time.Minute
	// enrichSearchTimeout bounds one web search so it cannot eat a lead's budget.
	enrichSearchTimeout = 30 * time.Second
	// enrichSaveTimeout is kept apart from the lead's budget so a slow read is still saved.
	enrichSaveTimeout = 15 * time.Second
)

// errOutOfBalance stops a run when the workspace cannot pay for the next site.
var errOutOfBalance = errors.New("the workspace balance ran out; the rest of the leads were left as they were")

// AuditLogger records a finished run so every teammate's dashboard refreshes.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// EnrichOptions are what an enrichment run does besides reading each site.
type EnrichOptions struct {
	Verify    bool
	People    bool
	WebSearch bool
}

// StartEnrichment queues a run reading the leads' websites and starts it in
// the background.
func (s *Service) StartEnrichment(ctx context.Context, orgID, userID uuid.UUID, ids []uuid.UUID, opts EnrichOptions) (*models.LeadEnrichmentRun, error) {
	if len(ids) == 0 {
		return nil, errx.New(errx.BadRequest, "select at least one lead to enrich")
	}
	uid := userID
	run := &models.LeadEnrichmentRun{OrganizationID: orgID, RequestedByUserID: &uid, LeadIDs: ids, Verify: opts.Verify, People: opts.People, WebSearch: opts.WebSearch, Total: len(ids)}
	if err := s.Repo.CreateEnrichmentRun(ctx, run); err != nil {
		return nil, err
	}
	if err := s.Repo.MarkQueued(ctx, orgID, ids); err != nil {
		return nil, err
	}
	go func(id uuid.UUID) {
		bg, cancel := context.WithTimeout(context.Background(), enrichRunTimeout)
		defer cancel()
		s.runEnrichment(bg, id)
	}(run.ID)
	return run, nil
}

// EnrichmentRun returns one run's progress.
func (s *Service) EnrichmentRun(ctx context.Context, orgID, id uuid.UUID) (*models.LeadEnrichmentRun, error) {
	run, err := s.Repo.GetEnrichmentRun(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, errx.New(errx.NotFound, "enrichment run not found")
	}
	return run, nil
}

// ActiveEnrichmentRuns lists the runs still going.
func (s *Service) ActiveEnrichmentRuns(ctx context.Context, orgID uuid.UUID) ([]models.LeadEnrichmentRun, error) {
	return s.Repo.ActiveEnrichmentRuns(ctx, orgID)
}

// RunPendingEnrichment picks up queued runs and runs a crashed process left behind.
func (s *Service) RunPendingEnrichment(ctx context.Context) error {
	ids, err := s.Repo.DueEnrichmentRuns(ctx, 10)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.runEnrichment(ctx, id)
	}
	return nil
}

func (s *Service) runEnrichment(ctx context.Context, id uuid.UUID) {
	run, err := s.Repo.ClaimEnrichmentRun(ctx, id)
	if err != nil || run == nil {
		if err != nil {
			log.Warn().Err(err).Str("run", id.String()).Msg("leadfinder: could not claim enrichment run")
		}
		return
	}
	err = s.enrich(ctx, run)
	run.Status = "done"
	if err != nil {
		run.Status = "error"
		var xe *errx.Error
		if errors.As(err, &xe) {
			run.LastError = xe.Message
		} else {
			run.LastError = err.Error()
		}
		if !errors.Is(err, errOutOfBalance) {
			log.Warn().Err(err).Str("run", id.String()).Msg("leadfinder: enrichment run failed")
		}
	}
	fin, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if ferr := s.Repo.FinishEnrichmentRun(fin, run); ferr != nil {
		log.Warn().Err(ferr).Str("run", id.String()).Msg("leadfinder: could not finish enrichment run")
	}
	if s.Audit != nil && run.RequestedByUserID != nil {
		s.Audit.LogAction(fin, run.OrganizationID, *run.RequestedByUserID, models.AuditActionUpdate, models.AuditEntityLead, nil, "", "",
			nil, map[string]string{"enrichment_run": run.ID.String(), "status": run.Status})
	}
}

// enrich walks the run's leads from where it stopped, a chunk at a time.
func (s *Service) enrich(ctx context.Context, run *models.LeadEnrichmentRun) error {
	tools := s.webTools(ctx, run.OrganizationID)
	if !run.WebSearch {
		tools.searches = nil
	}
	for run.Processed < len(run.LeadIDs) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		end := min(run.Processed+enrichChunk, len(run.LeadIDs))
		leads, err := s.Repo.LeadsByIDs(ctx, run.OrganizationID, run.LeadIDs[run.Processed:end])
		if err != nil {
			return err
		}
		var (
			mu      sync.Mutex
			wg      sync.WaitGroup
			stopErr error
		)
		sem := make(chan struct{}, enrichParallel)
		for i := range leads {
			wg.Add(1)
			sem <- struct{}{}
			go func(l *models.Lead) {
				defer wg.Done()
				defer func() { <-sem }()
				out, found, charged, err := s.enrichLead(ctx, run, tools, l)
				mu.Lock()
				defer mu.Unlock()
				run.PeopleFound += found
				run.ChargedMicro += charged
				switch {
				case errors.Is(err, errOutOfBalance):
					stopErr = err
				case err != nil || out == outcomeFailed:
					run.Failed++
				case out == outcomeNoWebsite:
					run.NoWebsite++
				}
				if err == nil && l.Email != "" {
					run.EmailsFound++
				}
			}(&leads[i])
		}
		wg.Wait()
		// A lead deleted since the run started counts as processed.
		run.Processed = end
		if err := s.Repo.UpdateEnrichmentProgress(ctx, run); err != nil {
			return err
		}
		if stopErr != nil {
			return stopErr
		}
	}
	return nil
}

type enrichOutcome int

const (
	outcomeNoWebsite enrichOutcome = iota
	outcomeRead
	outcomeFailed
)

// enrichLead reads one lead's website and returns what it found and what the read cost, net of a refund.
func (s *Service) enrichLead(ctx context.Context, run *models.LeadEnrichmentRun, tools webTools, l *models.Lead) (enrichOutcome, int, models.Micro, error) {
	timeout := enrichLeadTimeout
	if tools.crawler.fallback != nil || len(tools.searches) > 0 {
		timeout = enrichLeadSlowTimeout
	}
	lctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	l.OrganizationID = run.OrganizationID
	idem := run.ID.String() + ":" + l.ID.String()
	save := func() error {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), enrichSaveTimeout)
		defer cancel()
		return s.Repo.SaveEnrichment(sctx, l)
	}
	search := func(query, key string) []generation.SearchResult {
		sctx, cancel := context.WithTimeout(lctx, enrichSearchTimeout)
		defer cancel()
		return s.search(sctx, run.OrganizationID, tools.searches, query, idem+key)
	}
	found := l.Site.FoundWebsite
	if l.Website == "" {
		var searched []string
		if len(tools.searches) > 0 && strings.TrimSpace(l.Name) != "" {
			searched = searchedHosts(search(siteQuery(l), ":site"))
		}
		l.Website, l.Domain = tools.crawler.FindWebsite(lctx, l, searched)
		found = l.Website != ""
	}
	if l.Website == "" {
		l.EnrichmentStatus = models.LeadEnrichmentNoWebsite
		s.finishEmails(lctx, run, l, nil)
		return outcomeNoWebsite, 0, 0, save()
	}
	key := "lead_enrich:" + run.ID.String() + ":" + l.ID.String()
	var charged models.Micro
	if s.metered() {
		taken, err := s.Credits.ChargeUpfront(lctx, run.OrganizationID, credits.UnitUsage(instancesettings.UnitLeadWebsiteRead, 1), "lead_enrich", key)
		if err != nil {
			if errors.Is(err, credits.ErrInsufficientCredits) || errors.Is(err, credits.ErrCapExceeded) {
				return outcomeFailed, 0, 0, errOutOfBalance
			}
			return outcomeFailed, 0, 0, err
		}
		charged = taken
	}
	res, err := tools.crawler.Crawl(lctx, l.Website)
	if err != nil {
		now := time.Now()
		res = &CrawlResult{Site: models.LeadSite{Error: err.Error(), Unread: models.LeadSiteUnreachable, CheckedAt: &now}}
	}
	// A site that could not be read, or published no address, may have one
	// shown on pages about it.
	// The domain the site lands on after redirects is the one its addresses use.
	domain := res.Domain
	if domain == "" {
		domain = l.Domain
	}
	if len(tools.searches) > 0 && len(res.Emails) == 0 && len(l.Emails) == 0 && domain != "" {
		res.Emails = searchedEmails(search(`"@`+domain+`"`, ":emails"), domain)
	}
	// What earlier runs learned about the domain's addresses outlives this read.
	prev := l.Site
	l.Site = res.Site
	if l.Site.EmailPattern == "" {
		l.Site.EmailPattern = prev.EmailPattern
	}
	l.Site.AcceptsAll = prev.AcceptsAll
	l.Site.FoundWebsite = found
	outcome := outcomeRead
	if res.Site.Reachable {
		l.EnrichmentStatus = models.LeadEnrichmentDone
	} else {
		// A site that never answered costs nothing.
		l.EnrichmentStatus = models.LeadEnrichmentFailed
		outcome = outcomeFailed
		if s.metered() {
			refunded, rerr := s.Credits.RefundCharge(context.WithoutCancel(lctx), run.OrganizationID, key, "lead_enrich_refund")
			if rerr != nil {
				log.Warn().Err(rerr).Msg("leadfinder: could not refund an unreachable site")
			}
			charged = max(charged-refunded, 0)
		}
	}
	if l.Socials == nil {
		l.Socials = map[string]string{}
	}
	for k, v := range res.Socials {
		if l.Socials[k] == "" {
			l.Socials[k] = v
		}
	}
	if l.Phone == "" && len(res.Site.Phones) > 0 {
		l.Phone = res.Site.Phones[0]
	}
	s.finishEmails(lctx, run, l, res.Emails)
	people := 0
	if run.People && res.Site.Reachable {
		n, err := s.enrichPeople(lctx, run, l, res.People)
		if err != nil {
			log.Warn().Err(err).Str("lead", l.ID.String()).Msg("leadfinder: could not save a lead's people")
		}
		people = n
	}
	return outcome, people, charged, save()
}

// finishEmails merges found addresses into the lead, verifies the best few
// and picks the one to reach out to. A suppressed address is kept on the lead
// so nobody adds it again by hand, and is never the one picked.
func (s *Service) finishEmails(ctx context.Context, run *models.LeadEnrichmentRun, l *models.Lead, found []FoundEmail) {
	byEmail := map[string]bool{}
	var list []models.LeadEmail
	for _, e := range l.Emails {
		if e.Email != "" && !byEmail[e.Email] {
			list = append(list, e)
			byEmail[e.Email] = true
		}
	}
	for _, f := range found {
		if byEmail[f.Email] {
			continue
		}
		src := f.Source
		if src == "" {
			src = "website"
		}
		list = append(list, models.LeadEmail{Email: f.Email, Source: src, URL: f.URL, DomainMatch: emailDomainMatches(f.Email, l.Domain)})
		byEmail[f.Email] = true
	}
	if len(list) == 0 {
		l.Emails, l.Email = []models.LeadEmail{}, ""
		return
	}
	addrs := make([]string, 0, len(list))
	for _, e := range list {
		addrs = append(addrs, e.Email)
	}
	suppressed, err := s.Repo.SuppressedEmails(ctx, run.OrganizationID, addrs)
	if err != nil {
		log.Warn().Err(err).Msg("leadfinder: could not check suppression")
		suppressed = map[string]bool{}
		for _, a := range addrs {
			// Unknown means not usable: nothing is picked until the check works.
			suppressed[a] = true
		}
	}
	for i := range list {
		if suppressed[list[i].Email] {
			list[i].Status = "suppressed"
		} else if list[i].Status == "suppressed" {
			// No longer suppressed: verify it again like a new address.
			list[i].Status, list[i].SubStatus = "", ""
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return emailRank(list[i]) < emailRank(list[j]) })
	if run.Verify && s.Verifier != nil {
		checked := 0
		for i := range list {
			if checked >= enrichVerifyMax {
				break
			}
			if list[i].Status != "" {
				continue
			}
			r := s.Verifier.VerifyAddress(ctx, run.OrganizationID, list[i].Email)
			list[i].Status, list[i].SubStatus = string(r.Status), string(r.SubStatus)
			checked++
		}
		sort.SliceStable(list, func(i, j int) bool { return emailRank(list[i]) < emailRank(list[j]) })
	}
	l.Emails = list
	l.Email = ""
	if top := list[0]; top.Status != "suppressed" && top.Status != string(emailverify.StatusInvalid) {
		l.Email = top.Email
	}
}

// genericLocals read as a business's front desk, the right first contact.
var genericLocals = map[string]bool{"info": true, "hello": true, "contact": true, "office": true, "hi": true, "enquiries": true, "inquiries": true, "admin": true, "mail": true, "team": true}

// emailRank orders addresses best first: deliverable, on the business's own
// domain, a front desk before a named person's address picked off a page.
func emailRank(e models.LeadEmail) int {
	r := 0
	switch e.Status {
	case string(emailverify.StatusValid):
	case "", string(emailverify.StatusUnknown):
		r += 20
	case string(emailverify.StatusRisky):
		r += 30
	case string(emailverify.StatusInvalid):
		r += 200
	case "suppressed":
		r += 300
	}
	if !e.DomainMatch {
		r += 10
	}
	local := e.Email
	if at := strings.IndexByte(local, '@'); at >= 0 {
		local = local[:at]
	}
	if strings.HasPrefix(local, "noreply") || strings.HasPrefix(local, "no-reply") || strings.HasPrefix(local, "donotreply") {
		r += 100
	} else if !genericLocals[local] {
		r += 2
	}
	return r
}
