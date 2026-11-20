package apollo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// readCap bounds how many list contacts one run reads; a longer list
	// resumes where it stopped on the next run.
	readCap = 10000
	// importCap bounds how many new people one run imports.
	importCap = 5000
	// EnrichCap is the most contacts one enrichment run takes.
	EnrichCap = 5000
	// RecurringEvery is how often a "keep in sync" source re-reads its list.
	RecurringEvery = time.Hour
	// enrichChunk is how many contacts an enrichment run loads and saves at once.
	enrichChunk = 100
	// runTimeout bounds one import run; staleAfter is when a run still marked
	// running is treated as interrupted, and must stay above runTimeout.
	runTimeout = 25 * time.Minute
	staleAfter = 30 * time.Minute
	// finishTimeout bounds the write that records a run's outcome.
	finishTimeout = 15 * time.Second
)

// Deps is everything the Apollo integration talks to.
type Deps struct {
	Repo         repository.ApolloRepository
	Integrations integration.Service
	Contacts     contact.ContactService
}

// Service imports Apollo lists and enriches contacts through Apollo.
type Service struct {
	Deps
	newClient func(key string) *Client
}

// NewService builds the Apollo integration and registers its key check.
func NewService(d Deps) *Service {
	s := &Service{Deps: d, newClient: NewClient}
	if d.Integrations != nil {
		d.Integrations.SetCredentialCheck(models.IntegrationApollo, s.CheckKey)
	}
	return s
}

// CheckKey validates a pasted key against everything the integration uses and
// returns the Apollo account it acts as.
func (s *Service) CheckKey(ctx context.Context, config map[string]any) (map[string]any, error) {
	key, _ := config["api_key"].(string)
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("paste your Apollo API key")
	}
	config["api_key"] = key
	cl := s.newClient(key)
	if err := cl.Health(ctx); err != nil {
		return nil, plainErr(err)
	}
	if _, err := cl.Lists(ctx); err != nil {
		return nil, plainErr(err)
	}
	if _, _, _, err := cl.SearchContacts(ctx, "", 1, 1); err != nil {
		return nil, plainErr(err)
	}
	facts := map[string]any{}
	name, email := cl.Profile(ctx)
	if name != "" {
		facts["account_name"] = name
	}
	if email != "" {
		facts["account_email"] = email
	}
	return facts, nil
}

// plainErr turns an Apollo error into text a person can act on.
func plainErr(err error) error {
	var ae *APIError
	switch {
	case errors.Is(err, ErrKey), errors.Is(err, ErrForbidden), errors.Is(err, ErrRateLimited), errors.Is(err, ErrUnavailable):
		return errors.New(err.Error())
	case errors.As(err, &ae):
		if ae.Message != "" {
			return fmt.Errorf("Apollo refused the request: %s", truncate(ae.Message, 200))
		}
		return fmt.Errorf("Apollo refused the request (HTTP %d)", ae.Status)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return ErrUnavailable
	}
	return ErrUnavailable
}

// asErrx is the API answer for an Apollo failure.
func asErrx(err error) error {
	var xe *errx.Error
	if errors.As(err, &xe) {
		return xe
	}
	switch {
	case errors.Is(err, ErrKey):
		return errx.NewWithIdentifier(errx.Unprocessable, "apollo_key_rejected", ErrKey.Error()+". Replace the key on the Apollo page.")
	case errors.Is(err, ErrForbidden):
		return errx.NewWithIdentifier(errx.Unprocessable, "apollo_key_forbidden", ErrForbidden.Error())
	case errors.Is(err, ErrRateLimited):
		return errx.NewWithIdentifier(errx.TooManyRequests, "apollo_rate_limited", ErrRateLimited.Error())
	}
	return errx.New(errx.Unprocessable, plainErr(err).Error())
}

// conn opens an org's Apollo connection and a client for its key.
func (s *Service) conn(ctx context.Context, orgID, connID uuid.UUID) (*Client, *models.IntegrationConnection, error) {
	cfg, c, err := s.Integrations.ConnectionConfig(ctx, orgID, connID)
	if err != nil {
		if errors.Is(err, integration.ErrConnectionNotFound) {
			return nil, nil, errx.New(errx.NotFound, "Apollo connection not found")
		}
		return nil, nil, err
	}
	if c.Provider != models.IntegrationApollo {
		return nil, nil, errx.New(errx.NotFound, "Apollo connection not found")
	}
	key, _ := cfg["api_key"].(string)
	if strings.TrimSpace(key) == "" {
		return nil, nil, errx.NewWithIdentifier(errx.Unprocessable, "apollo_key_missing", "Add your Apollo API key on the Apollo page first")
	}
	return s.newClient(strings.TrimSpace(key)), c, nil
}

// noteHealth records what an Apollo call says about the connection.
func (s *Service) noteHealth(ctx context.Context, connID uuid.UUID, err error) {
	switch {
	case err == nil:
		s.Integrations.MarkConnectionHealth(ctx, connID, models.IntegrationStatusConnected, models.IntegrationHealthHealthy, "")
	case errors.Is(err, ErrKey):
		s.Integrations.MarkConnectionHealth(ctx, connID, models.IntegrationStatusReauthRequired, models.IntegrationHealthDown, ErrKey.Error())
	case errors.Is(err, ErrForbidden):
		s.Integrations.MarkConnectionHealth(ctx, connID, models.IntegrationStatusConnected, models.IntegrationHealthDegraded, ErrForbidden.Error())
	}
}

// Overview is the Apollo page header.
func (s *Service) Overview(ctx context.Context, orgID, connID uuid.UUID) (*models.ApolloOverview, error) {
	c, err := s.Integrations.GetConnection(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.Provider != models.IntegrationApollo {
		return nil, errx.New(errx.NotFound, "Apollo connection not found")
	}
	out := &models.ApolloOverview{
		ConnectionID: c.ID, Label: c.Label, Status: c.Status, Health: c.Health,
	}
	if c.HealthDetail != nil {
		out.HealthDetail = *c.HealthDetail
	}
	var df map[string]any
	if len(c.DisplayFields) > 0 {
		_ = jsonUnmarshal(c.DisplayFields, &df)
	}
	out.AccountName, _ = df["account_name"].(string)
	out.AccountEmail, _ = df["account_email"].(string)
	sources, err := s.Repo.ListSources(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	out.Sources = len(sources)
	for _, src := range sources {
		out.TotalImported += src.TotalImported
		if src.LastRunAt != nil && (out.LastRunAt == nil || src.LastRunAt.After(*out.LastRunAt)) {
			t := *src.LastRunAt
			out.LastRunAt = &t
		}
	}
	return out, nil
}

// ReplaceKey checks a new key and swaps it in.
func (s *Service) ReplaceKey(ctx context.Context, orgID, connID uuid.UUID, key string) (*models.ApolloOverview, error) {
	if _, _, err := s.Integrations.ConnectionConfig(ctx, orgID, connID); err != nil {
		if errors.Is(err, integration.ErrConnectionNotFound) {
			return nil, errx.New(errx.NotFound, "Apollo connection not found")
		}
		return nil, err
	}
	c, err := s.Integrations.GetConnection(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.Provider != models.IntegrationApollo {
		return nil, errx.New(errx.NotFound, "Apollo connection not found")
	}
	cfg := map[string]any{"api_key": key}
	facts, err := s.CheckKey(ctx, cfg)
	if err != nil {
		return nil, errx.New(errx.Unprocessable, err.Error())
	}
	if err := s.Integrations.MergeConnectionConfig(ctx, orgID, connID, map[string]any{"api_key": cfg["api_key"]}); err != nil {
		return nil, err
	}
	patch := map[string]any{"account_name": facts["account_name"], "account_email": facts["account_email"]}
	_ = s.Integrations.MergeConnectionDisplay(ctx, connID, patch)
	s.noteHealth(ctx, connID, nil)
	return s.Overview(ctx, orgID, connID)
}

// Lists returns the account's saved contact lists.
func (s *Service) Lists(ctx context.Context, orgID, connID uuid.UUID) ([]models.ApolloList, error) {
	cl, c, err := s.conn(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	lists, err := cl.Lists(ctx)
	s.noteHealth(ctx, c.ID, err)
	if err != nil {
		return nil, asErrx(err)
	}
	return lists, nil
}

// Preview reads the first people of a list without importing.
func (s *Service) Preview(ctx context.Context, orgID, connID uuid.UUID, listID string, verifiedOnly bool) (*models.ApolloPreview, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return nil, errx.New(errx.BadRequest, "list_id is required")
	}
	cl, c, err := s.conn(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	people, total, _, err := cl.SearchContacts(ctx, listID, 1, 25)
	s.noteHealth(ctx, c.ID, err)
	if err != nil {
		return nil, asErrx(err)
	}
	emails := make([]string, 0, len(people))
	for _, p := range people {
		if p.Email != "" {
			emails = append(emails, p.Email)
		}
	}
	have, err := s.Repo.ContactsByEmails(ctx, orgID, emails)
	if err != nil {
		return nil, err
	}
	suppressed, err := s.Repo.SuppressedEmails(ctx, orgID, emails)
	if err != nil {
		return nil, err
	}
	out := &models.ApolloPreview{Total: total, Sample: make([]models.ApolloPreviewRow, 0, len(people))}
	for _, p := range people {
		addr := normEmail(p.Email)
		_, inWB := have[addr]
		out.Sample = append(out.Sample, models.ApolloPreviewRow{
			ApolloID: p.ID, Name: p.DisplayName(), Email: p.Email, EmailStatus: p.EmailStatus,
			Title: strings.TrimSpace(p.Title), Company: p.Company(), AlreadyInWB: inWB,
			WillBeImport: addr != "" && !suppressed[addr] && (!verifiedOnly || isVerified(p.EmailStatus)),
		})
	}
	return out, nil
}

// SourceInput creates or edits an import source.
type SourceInput struct {
	Name          *string     `json:"name"`
	ListID        string      `json:"list_id"`
	ListName      string      `json:"list_name"`
	CampaignID    *uuid.UUID  `json:"campaign_id"`
	ClearCampaign bool        `json:"-"`
	CategoryIDs   []uuid.UUID `json:"category_ids"`
	SegmentIDs    []uuid.UUID `json:"segment_ids"`
	VerifiedOnly  *bool       `json:"verified_only"`
	Recurring     *bool       `json:"recurring"`
	Enabled       *bool       `json:"enabled"`
}

// CreateSource saves a list import and starts its first run.
func (s *Service) CreateSource(ctx context.Context, orgID, connID, userID uuid.UUID, in SourceInput) (*models.ApolloImportSource, error) {
	listID := strings.TrimSpace(in.ListID)
	if listID == "" || len(listID) > 64 {
		return nil, errx.New(errx.BadRequest, "list_id is required")
	}
	if len(in.CategoryIDs) > 50 {
		return nil, errx.New(errx.BadRequest, "at most 50 tags")
	}
	if len(in.SegmentIDs) > models.ImportSourceSegmentsMax {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d segments", models.ImportSourceSegmentsMax))
	}
	if _, _, err := s.conn(ctx, orgID, connID); err != nil {
		return nil, err
	}
	listName := truncate(strings.TrimSpace(in.ListName), 200)
	name := listName
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = truncate(strings.TrimSpace(*in.Name), 200)
	}
	if name == "" {
		name = "Apollo list"
	}
	uid := userID
	row := &models.ApolloImportSource{
		OrganizationID:  orgID,
		ConnectionID:    connID,
		CreatedByUserID: &uid,
		Name:            name,
		ListID:          listID,
		ListName:        listName,
		CampaignID:      in.CampaignID,
		CategoryIDs:     in.CategoryIDs,
		SegmentIDs:      in.SegmentIDs,
		VerifiedOnly:    in.VerifiedOnly == nil || *in.VerifiedOnly,
		Recurring:       in.Recurring != nil && *in.Recurring,
		Enabled:         true,
	}
	if err := s.Repo.CreateSource(ctx, row); err != nil {
		if errors.Is(err, repository.ErrApolloSourceRefs) {
			return nil, errx.New(errx.BadRequest, "The campaign to enroll into, a label or a segment was not found in this workspace")
		}
		return nil, err
	}
	return s.StartRun(ctx, orgID, row.ID)
}

// UpdateSource edits a source's targets, filter and schedule.
func (s *Service) UpdateSource(ctx context.Context, orgID, id uuid.UUID, in SourceInput) (*models.ApolloImportSource, error) {
	src, err := s.Repo.GetSource(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errx.New(errx.NotFound, "import source not found")
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		src.Name = truncate(strings.TrimSpace(*in.Name), 200)
	}
	if in.ClearCampaign {
		src.CampaignID = nil
	} else if in.CampaignID != nil {
		src.CampaignID = in.CampaignID
	}
	if in.CategoryIDs != nil {
		if len(in.CategoryIDs) > 50 {
			return nil, errx.New(errx.BadRequest, "at most 50 tags")
		}
		src.CategoryIDs = in.CategoryIDs
	}
	if in.SegmentIDs != nil {
		if len(in.SegmentIDs) > models.ImportSourceSegmentsMax {
			return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d segments", models.ImportSourceSegmentsMax))
		}
		src.SegmentIDs = in.SegmentIDs
	}
	if in.VerifiedOnly != nil {
		src.VerifiedOnly = *in.VerifiedOnly
	}
	if in.Recurring != nil {
		src.Recurring = *in.Recurring
	}
	if in.Enabled != nil {
		src.Enabled = *in.Enabled
	}
	if err := s.Repo.UpdateSource(ctx, src); err != nil {
		if errors.Is(err, repository.ErrApolloSourceRefs) {
			return nil, errx.New(errx.BadRequest, "The campaign to enroll into, a label or a segment was not found in this workspace")
		}
		return nil, err
	}
	return s.Repo.GetSource(ctx, orgID, id)
}

// StartRun claims a source and runs it in the background.
func (s *Service) StartRun(ctx context.Context, orgID, id uuid.UUID) (*models.ApolloImportSource, error) {
	src, err := s.Repo.GetSource(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errx.New(errx.NotFound, "import source not found")
	}
	claimed, err := s.Repo.ClaimSource(ctx, id)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, errx.NewWithIdentifier(errx.Conflict, "import_running", "This import is already running.")
	}
	go func(src models.ApolloImportSource) {
		bg, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()
		s.runSource(bg, src)
	}(*src)
	src.Status = "running"
	return src, nil
}

// RunRecurring runs every "keep in sync" source that is due.
func (s *Service) RunRecurring(ctx context.Context) error {
	if _, err := s.Repo.ReleaseStaleSources(ctx, staleAfter); err != nil {
		log.Warn().Err(err).Msg("apollo: could not release interrupted imports")
	}
	due, err := s.Repo.DueRecurringSources(ctx, RecurringEvery)
	if err != nil {
		return err
	}
	for _, src := range due {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		claimed, err := s.Repo.ClaimSource(ctx, src.ID)
		if err != nil || !claimed {
			continue
		}
		run, cancel := context.WithTimeout(ctx, runTimeout)
		s.runSource(run, src)
		cancel()
	}
	return nil
}

func (s *Service) runSource(ctx context.Context, src models.ApolloImportSource) {
	res, err := s.importOnce(ctx, src)
	msg := ""
	if err != nil {
		var xe *errx.Error
		if errors.As(err, &xe) {
			msg = xe.Message
		} else {
			msg = plainErr(err).Error()
		}
		log.Warn().Err(err).Str("source", src.ID.String()).Msg("apollo: import run failed")
	}
	// The outcome is recorded even when the run's own context has ended.
	fin, cancel := finishContext(ctx)
	defer cancel()
	if err := s.Repo.FinishSource(fin, src.ID, res, msg); err != nil {
		log.Warn().Err(err).Str("source", src.ID.String()).Msg("apollo: could not record import result")
	}
}

// finishContext outlives the run's context but is bounded on its own, so a
// stalled final write cannot hold up the loop.
func finishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
}

// importOnce reads the list, imports people it has not brought in before and
// remembers each of them for the next run.
func (s *Service) importOnce(ctx context.Context, src models.ApolloImportSource) (*models.ApolloRunResult, error) {
	cl, c, err := s.conn(ctx, src.OrganizationID, src.ConnectionID)
	if err != nil {
		return nil, err
	}
	start := 1
	if src.LastResult != nil && src.LastResult.NextPage > 1 {
		start = src.LastResult.NextPage
	}
	res := &models.ApolloRunResult{}
	var people []Person
	page := start
	for {
		batch, _, totalPages, err := cl.SearchContacts(ctx, src.ListID, page, SearchPageSize)
		if err != nil {
			s.noteHealth(ctx, c.ID, err)
			if len(people) == 0 {
				return nil, err
			}
			// Keep what was read; the next run resumes at this page.
			res.Truncated, res.NextPage = true, page
			break
		}
		people = append(people, batch...)
		if len(batch) == 0 || page >= totalPages {
			break
		}
		page++
		if len(people) >= readCap {
			res.Truncated, res.NextPage = true, page
			break
		}
	}
	if !res.Truncated {
		s.noteHealth(ctx, c.ID, nil)
	}
	res.Read = len(people)

	ids := make([]string, 0, len(people))
	emails := make([]string, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.ID)
		if p.Email != "" {
			emails = append(emails, p.Email)
		}
	}
	seen, err := s.Repo.SourceMembers(ctx, src.ID, ids)
	if err != nil {
		return nil, err
	}
	suppressed, err := s.Repo.SuppressedEmails(ctx, src.OrganizationID, emails)
	if err != nil {
		return nil, err
	}
	var fresh []Person
	dup := map[string]bool{}
	for _, p := range people {
		addr := normEmail(p.Email)
		switch {
		case p.ID == "" || seen[p.ID] || dup[p.ID]:
			res.AlreadySeen++
		case addr == "":
			// Not remembered: once Apollo has an address, they come in.
			res.NoEmail++
		case src.VerifiedOnly && !isVerified(p.EmailStatus):
			res.Unverified++
		case suppressed[addr]:
			res.Suppressed++
		case len(fresh) >= importCap:
			res.Truncated = true
			if res.NextPage == 0 {
				res.NextPage = start
			}
		default:
			dup[p.ID] = true
			fresh = append(fresh, p)
		}
	}
	if len(fresh) == 0 {
		return res, nil
	}

	actor := uuid.Nil
	if src.CreatedByUserID != nil {
		actor = *src.CreatedByUserID
	} else if c.ConnectedByUserID != nil {
		actor = *c.ConnectedByUserID
	}
	rows, mapping := importRows(fresh)
	var campaigns []string
	if src.CampaignID != nil {
		campaigns = []string{src.CampaignID.String()}
	}
	cats := make([]string, 0, len(src.CategoryIDs))
	for _, id := range src.CategoryIDs {
		cats = append(cats, id.String())
	}
	segs := make([]string, 0, len(src.SegmentIDs))
	for _, id := range src.SegmentIDs {
		segs = append(segs, id.String())
	}
	subscribed := true
	opts := &models.ContactImportCommit{
		Mapping:             mapping,
		Dedup:               models.ContactImportDedupUpdate,
		CategoryIDs:         cats,
		CampaignIDs:         campaigns,
		SegmentIDs:          segs,
		SkipMissingSegments: true,
		SubscribedDefault:   &subscribed,
		Source:              models.ContactSourceApollo,
		SourceDetail:        truncate("Apollo: "+orDefault(src.ListName, src.Name), 200),
	}
	sink := newCollectSink()
	result, xerr := s.Contacts.RunImport(ctx, actor.String(), src.OrganizationID, rows, opts, sink, nil)
	if xerr != nil {
		return res, xerr
	}
	res.Imported, res.Updated, res.Skipped, res.Failed = result.Imported, result.Updated, result.Skipped, result.Failed

	members := map[string]uuid.UUID{}
	for i, p := range fresh {
		o, ok := sink.outcomes[i+1]
		if !ok || o.ContactID == nil || o.Status == models.ContactImportRowFailed {
			continue
		}
		members[p.ID] = *o.ContactID
	}
	_ = s.Repo.AddSourceMembers(ctx, src.ID, members)
	return res, nil
}

// importColumns are the contact fields an Apollo person fills, in cell order.
var importColumns = []struct {
	target models.ContactImportColumnTarget
	custom string
	value  func(p Person) string
}{
	{models.ContactImportTargetEmail, "", func(p Person) string { return strings.TrimSpace(p.Email) }},
	{models.ContactImportTargetFirstName, "", func(p Person) string { return strings.TrimSpace(p.FirstName) }},
	{models.ContactImportTargetLastName, "", func(p Person) string { return strings.TrimSpace(p.LastName) }},
	{models.ContactImportTargetCompany, "", Person.Company},
	{models.ContactImportTargetPhone, "", Person.Phone},
	{models.ContactImportTargetCustom, "title", func(p Person) string { return strings.TrimSpace(p.Title) }},
	{models.ContactImportTargetCustom, "seniority", func(p Person) string { return strings.TrimSpace(p.Seniority) }},
	{models.ContactImportTargetCustom, "linkedin_url", func(p Person) string { return strings.TrimSpace(p.LinkedinURL) }},
	{models.ContactImportTargetCustom, "city", func(p Person) string { return strings.TrimSpace(p.City) }},
	{models.ContactImportTargetCustom, "state", func(p Person) string { return strings.TrimSpace(p.State) }},
	{models.ContactImportTargetCustom, "country", func(p Person) string { return strings.TrimSpace(p.Country) }},
	{models.ContactImportTargetCustom, "website", Person.Website},
	{models.ContactImportTargetCustom, "company_domain", Person.CompanyDomain},
	{models.ContactImportTargetCustom, "industry", Person.Industry},
	{models.ContactImportTargetCustom, "apollo_id", func(p Person) string { return p.ID }},
	{models.ContactImportTargetCustom, "apollo_email_status", func(p Person) string { return strings.TrimSpace(p.EmailStatus) }},
}

// CustomFields are the contact custom fields an Apollo import or enrichment fills.
func CustomFields() []string {
	out := []string{}
	for _, c := range importColumns {
		if c.custom != "" {
			out = append(out, c.custom)
		}
	}
	return out
}

func importMapping() []models.ContactImportColumnMapping {
	mapping := make([]models.ContactImportColumnMapping, len(importColumns))
	for i, c := range importColumns {
		mapping[i] = models.ContactImportColumnMapping{Index: i, Target: c.target, CustomKey: c.custom}
	}
	return mapping
}

// importRows lays people out as the contact importer's rows; Line is the
// person's index plus one.
func importRows(people []Person) ([]contact.ImportRow, []models.ContactImportColumnMapping) {
	rows := make([]contact.ImportRow, len(people))
	for i, p := range people {
		cells := make([]string, len(importColumns))
		for j, c := range importColumns {
			cells[j] = truncate(c.value(p), 500)
		}
		rows[i] = contact.ImportRow{Line: i + 1, Cells: cells}
	}
	return rows, importMapping()
}

// collectSink keeps every row outcome of an import by line.
type collectSink struct {
	outcomes map[int]models.ContactImportRowOutcome
}

func newCollectSink() *collectSink {
	return &collectSink{outcomes: map[int]models.ContactImportRowOutcome{}}
}

func (c *collectSink) Settle(_ context.Context, outcomes []models.ContactImportRowOutcome) error {
	for _, o := range outcomes {
		c.outcomes[o.Line] = o
	}
	return nil
}

func (c *collectSink) Cancelled(context.Context) bool { return false }

func isVerified(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "verified")
}

func normEmail(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !strings.Contains(s, "@") {
		return ""
	}
	return s
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return d
}
