package toolmigrate

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
	"github.com/warmbly/warmbly/internal/repository"
)

// memRepo is an in-memory ToolMigrationRepository with the same scoping and claim rules.
type memRepo struct {
	mu        sync.Mutex
	migs      map[uuid.UUID]*memMig
	targets   map[uuid.UUID]*memTarget
	order     []uuid.UUID
	items     map[string]memItem
	mailboxes map[string]uuid.UUID
	// onClaimStart runs inside ClaimStart, to stand in for a racing caller.
	onClaimStart func()
}

type memItem struct {
	id      uuid.UUID
	pending bool
}

type memMig struct {
	m        models.ToolMigration
	sealed   *string
	via      bool
	attempts int
	token    uuid.UUID
	options  []byte
	starting bool
	created  map[string]uuid.UUID
}

type memTarget struct {
	t        models.ToolMigrationTarget
	cursor   []byte
	attempts int
	token    uuid.UUID
}

func newMemRepo() *memRepo {
	return &memRepo{migs: map[uuid.UUID]*memMig{}, targets: map[uuid.UUID]*memTarget{}, items: map[string]memItem{}, mailboxes: map[string]uuid.UUID{}}
}

func (r *memRepo) inScope(scope repository.ToolMigrationScope, m *models.ToolMigration) bool {
	switch {
	case scope.OrgID != nil:
		return m.OrganizationID != nil && *m.OrganizationID == *scope.OrgID
	case scope.AgencyID != nil:
		return m.AgencyID != nil && *m.AgencyID == *scope.AgencyID
	}
	return false
}

func (r *memRepo) Create(_ context.Context, m *models.ToolMigration, sealed string, via bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m.CreatedAt, m.UpdatedAt = time.Now(), time.Now()
	s := sealed
	r.migs[m.ID] = &memMig{m: *m, sealed: &s, via: via, created: map[string]uuid.UUID{}}
	return nil
}

func (r *memRepo) snapshot(mm *memMig) *models.ToolMigration {
	out := mm.m
	out.CredentialsHeld = mm.sealed != nil
	out.Targets = []models.ToolMigrationTarget{}
	for _, id := range r.order {
		if t := r.targets[id]; t.t.MigrationID == out.ID {
			out.Targets = append(out.Targets, t.t)
		}
	}
	if len(mm.options) > 0 {
		var o models.ToolMigrationOptions
		_ = json.Unmarshal(mm.options, &o)
		out.Options = &o
	}
	return &out
}

func (r *memRepo) Get(_ context.Context, scope repository.ToolMigrationScope, id uuid.UUID) (*models.ToolMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mm, ok := r.migs[id]
	if !ok || !r.inScope(scope, &mm.m) {
		return nil, nil
	}
	return r.snapshot(mm), nil
}

func (r *memRepo) List(_ context.Context, scope repository.ToolMigrationScope, _ *time.Time, _ *uuid.UUID, limit int) ([]models.ToolMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []models.ToolMigration{}
	for _, mm := range r.migs {
		if r.inScope(scope, &mm.m) && len(out) < limit {
			out = append(out, *r.snapshot(mm))
		}
	}
	return out, nil
}

func (r *memRepo) CountActive(_ context.Context, scope repository.ToolMigrationScope) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, mm := range r.migs {
		if r.inScope(scope, &mm.m) && (mm.m.Status.Active() || mm.m.Status == models.ToolMigrationReady) {
			n++
		}
	}
	return n, nil
}

func (r *memRepo) ClaimScan(context.Context, time.Duration) (*repository.ToolMigrationScanJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, mm := range r.migs {
		if mm.m.Status == models.ToolMigrationScanning && mm.sealed != nil && mm.token == uuid.Nil {
			mm.attempts++
			mm.token = uuid.New()
			return &repository.ToolMigrationScanJob{ID: id, Token: mm.token, Provider: string(mm.m.Provider), Sealed: *mm.sealed, Attempts: mm.attempts}, nil
		}
	}
	return nil, nil
}

func (r *memRepo) RenewScan(_ context.Context, id, token uuid.UUID, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mm := r.migs[id]
	return mm != nil && mm.token == token && mm.m.Status == models.ToolMigrationScanning, nil
}

func (r *memRepo) FinishScan(_ context.Context, id, token uuid.UUID, inv []byte, status models.ToolMigrationStatus, msg string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mm := r.migs[id]
	if mm == nil || mm.token != token || mm.m.Status != models.ToolMigrationScanning {
		return false, nil
	}
	mm.m.Status, mm.m.Error, mm.token = status, msg, uuid.Nil
	if inv != nil {
		_ = json.Unmarshal(inv, &mm.m.Inventory)
	}
	if status != models.ToolMigrationReady {
		mm.sealed = nil
	}
	return true, nil
}

func (r *memRepo) ClaimStart(_ context.Context, scope repository.ToolMigrationScope, id uuid.UUID, _ time.Duration) (bool, map[string]uuid.UUID, error) {
	if r.onClaimStart != nil {
		r.onClaimStart()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	mm := r.migs[id]
	if mm == nil || !r.inScope(scope, &mm.m) || mm.m.Status != models.ToolMigrationReady || mm.sealed == nil || mm.starting {
		return false, nil, nil
	}
	mm.starting = true
	out := map[string]uuid.UUID{}
	for k, v := range mm.created {
		out[k] = v
	}
	return true, out, nil
}

func (r *memRepo) RecordClient(_ context.Context, id uuid.UUID, key string, orgID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mm := r.migs[id]; mm != nil {
		mm.created[key] = orgID
	}
	return nil
}

func (r *memRepo) ReleaseStart(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mm := r.migs[id]; mm != nil {
		mm.starting = false
	}
	return nil
}

func (r *memRepo) Start(_ context.Context, scope repository.ToolMigrationScope, id uuid.UUID, options []byte, targets []repository.ToolMigrationTargetInsert) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mm := r.migs[id]
	if mm == nil || !r.inScope(scope, &mm.m) || mm.m.Status != models.ToolMigrationReady || mm.sealed == nil || !mm.starting {
		return false, nil
	}
	mm.m.Status, mm.options, mm.starting = models.ToolMigrationRunning, options, false
	for _, t := range targets {
		r.targets[t.ID] = &memTarget{t: models.ToolMigrationTarget{
			ID: t.ID, MigrationID: id, OrganizationID: t.OrganizationID, Status: models.ToolMigrationTargetPending,
			Workspace: source.Workspace{ID: t.WorkspaceID, Key: t.KeyIndex, Name: t.WorkspaceName},
		}}
		r.order = append(r.order, t.ID)
	}
	return true, nil
}

func (r *memRepo) Cancel(_ context.Context, scope repository.ToolMigrationScope, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mm := r.migs[id]
	if mm == nil || !r.inScope(scope, &mm.m) || !(mm.m.Status.Active() || mm.m.Status == models.ToolMigrationReady) {
		return false, nil
	}
	mm.m.Status, mm.sealed = models.ToolMigrationCanceled, nil
	for _, t := range r.targets {
		if t.t.MigrationID == id && (t.t.Status == models.ToolMigrationTargetPending || t.t.Status == models.ToolMigrationTargetRunning) {
			t.t.Status = models.ToolMigrationTargetCanceled
		}
	}
	return true, nil
}

func (r *memRepo) Delete(_ context.Context, scope repository.ToolMigrationScope, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mm := r.migs[id]
	if mm == nil || !r.inScope(scope, &mm.m) || mm.m.Status == models.ToolMigrationRunning {
		return false, nil
	}
	delete(r.migs, id)
	for tid, t := range r.targets {
		if t.t.MigrationID == id {
			delete(r.targets, tid)
		}
	}
	return true, nil
}

func (r *memRepo) ClaimTarget(context.Context, time.Duration) (*repository.ToolMigrationTargetJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.order {
		t := r.targets[id]
		if t == nil {
			continue
		}
		mm := r.migs[t.t.MigrationID]
		if mm == nil || mm.m.Status != models.ToolMigrationRunning || t.t.Status != models.ToolMigrationTargetPending {
			continue
		}
		t.t.Status = models.ToolMigrationTargetRunning
		t.attempts++
		t.token = uuid.New()
		counts, _ := json.Marshal(t.t.Counts)
		followup, _ := json.Marshal(t.t.Followup)
		return &repository.ToolMigrationTargetJob{
			ID: id, MigrationID: mm.m.ID, OrgID: t.t.OrganizationID, WorkspaceID: t.t.Workspace.ID, KeyIndex: t.t.Workspace.Key,
			WorkspaceName: t.t.Workspace.Name, Phase: string(t.t.Phase), Cursor: t.cursor, Counts: counts, Followup: followup,
			MailboxImportID: t.t.MailboxImportID, CategoryID: t.t.CategoryID, Attempts: t.attempts, Token: t.token,
			Provider: string(mm.m.Provider), Label: mm.m.Label, Sealed: mm.sealed, Options: mm.options, ViaSession: mm.via,
			CreatedBy: mm.m.CreatedByUserID, AgencyID: mm.m.AgencyID,
		}, nil
	}
	return nil, nil
}

func (r *memRepo) apply(t *memTarget, p repository.ToolMigrationProgress) {
	t.t.Phase, t.cursor = source.Kind(p.Phase), p.Cursor
	if p.Counts != nil {
		_ = json.Unmarshal(p.Counts, &t.t.Counts)
	}
	if p.Followup != nil {
		t.t.Followup = models.ToolMigrationFollowup{}
		_ = json.Unmarshal(p.Followup, &t.t.Followup)
	}
	if p.MailboxImportID != nil {
		t.t.MailboxImportID = p.MailboxImportID
	}
	if p.CategoryID != nil {
		t.t.CategoryID = p.CategoryID
	}
}

// held reports whether a target is running under this token; the caller holds mu.
func (r *memRepo) held(id, token uuid.UUID) bool {
	t := r.targets[id]
	return t != nil && t.token == token && t.t.Status == models.ToolMigrationTargetRunning
}

func (r *memRepo) RenewTarget(_ context.Context, id, token uuid.UUID, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.held(id, token), nil
}

func (r *memRepo) SaveTarget(_ context.Context, id, token uuid.UUID, _ time.Duration, p repository.ToolMigrationProgress) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held(id, token) {
		return false, nil
	}
	r.apply(r.targets[id], p)
	return true, nil
}

func (r *memRepo) FinishTarget(_ context.Context, id, token uuid.UUID, status models.ToolMigrationTargetStatus, p repository.ToolMigrationProgress, msg string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held(id, token) {
		return false, nil
	}
	t := r.targets[id]
	r.apply(t, p)
	t.cursor, t.token = nil, uuid.Nil
	t.t.Status, t.t.Error = status, msg
	return true, nil
}

func (r *memRepo) Finalize(_ context.Context, id uuid.UUID) (models.ToolMigrationStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mm := r.migs[id]
	if mm == nil || mm.m.Status != models.ToolMigrationRunning {
		return "", nil
	}
	done := false
	for _, t := range r.targets {
		if t.t.MigrationID != id {
			continue
		}
		switch t.t.Status {
		case models.ToolMigrationTargetPending, models.ToolMigrationTargetRunning:
			return "", nil
		case models.ToolMigrationTargetDone:
			done = true
		}
	}
	mm.m.Status, mm.sealed = models.ToolMigrationFailed, nil
	if done {
		mm.m.Status = models.ToolMigrationDone
	}
	return mm.m.Status, nil
}

func itemKey(target uuid.UUID, kind, src string) string {
	return target.String() + "|" + kind + "|" + src
}

func (r *memRepo) Item(_ context.Context, target uuid.UUID, kind, src string) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	it, ok := r.items[itemKey(target, kind, src)]
	if !ok || it.pending {
		return uuid.Nil, false, nil
	}
	return it.id, true, nil
}

func (r *memRepo) ReserveItem(_ context.Context, target, token, _ uuid.UUID, kind, src string) (repository.ToolMigrationItemState, uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held(target, token) {
		return repository.ItemLost, uuid.Nil, nil
	}
	it, ok := r.items[itemKey(target, kind, src)]
	switch {
	case !ok:
		r.items[itemKey(target, kind, src)] = memItem{id: uuid.New(), pending: true}
		return repository.ItemReserved, uuid.Nil, nil
	case it.pending:
		return repository.ItemPending, uuid.Nil, nil
	}
	return repository.ItemDone, it.id, nil
}

func (r *memRepo) CompleteItem(_ context.Context, target, token uuid.UUID, kind, src string, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held(target, token) {
		return false, nil
	}
	r.items[itemKey(target, kind, src)] = memItem{id: id}
	return true, nil
}

func (r *memRepo) PutItems(_ context.Context, target, token, _ uuid.UUID, kind string, items map[string]uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held(target, token) {
		return false, nil
	}
	for s, id := range items {
		if _, ok := r.items[itemKey(target, kind, s)]; !ok {
			r.items[itemKey(target, kind, s)] = memItem{id: id}
		}
	}
	return true, nil
}

func (r *memRepo) ItemsIn(_ context.Context, target uuid.UUID, kind string, srcs []string) (map[string]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]bool{}
	for _, s := range srcs {
		if it, ok := r.items[itemKey(target, kind, s)]; ok && !it.pending {
			out[s] = true
		}
	}
	return out, nil
}

func (r *memRepo) CountItems(_ context.Context, target uuid.UUID, kind string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	prefix := target.String() + "|" + kind + "|"
	for k := range r.items {
		if strings.HasPrefix(k, prefix) {
			n++
		}
	}
	return n, nil
}

func (r *memRepo) SetFollowup(_ context.Context, mig, target uuid.UUID, raw []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t := r.targets[target]; t != nil && t.t.MigrationID == mig {
		t.t.Followup = models.ToolMigrationFollowup{}
		_ = json.Unmarshal(raw, &t.t.Followup)
	}
	return nil
}

func (r *memRepo) MailboxIDsByEmail(_ context.Context, _ uuid.UUID, emails []string) (map[string]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]uuid.UUID{}
	for _, e := range emails {
		if id, ok := r.mailboxes[e]; ok {
			out[e] = id
		}
	}
	return out, nil
}

func (r *memRepo) Upkeep(context.Context, time.Duration, int) ([]uuid.UUID, error) { return nil, nil }

// fakeProvider serves fixed data and records what it was asked.
type fakeProvider struct {
	info      source.Info
	rejectKey bool
	workspace []source.Workspace
	mailboxes []source.Mailbox
	campaigns []source.Campaign
	// leads by campaign id; "" is the account-level listing. A missing key is unsupported.
	leads     map[string][]source.Lead
	blocklist []source.BlockEntry
	// onCampaigns and onLeads run inside those calls, to stand in for something happening meanwhile.
	onCampaigns func()
	onLeads     func(ctx context.Context) error
}

func (p *fakeProvider) Info() source.Info { return p.info }

func (p *fakeProvider) Open(_ context.Context, creds source.Credentials) (source.Session, error) {
	if p.rejectKey || creds["api_key"] == "bad" {
		return nil, source.ErrKey
	}
	return p, nil
}

func (p *fakeProvider) Account(context.Context) (source.Account, error) {
	return source.Account{Name: "Dana", Email: "dana@example.com"}, nil
}

func (p *fakeProvider) Workspaces(context.Context) ([]source.Workspace, error) {
	if p.workspace != nil {
		return p.workspace, nil
	}
	return []source.Workspace{{Name: "Main"}}, nil
}

func (p *fakeProvider) Inventory(context.Context, source.Workspace) (source.Inventory, error) {
	return source.Inventory{Mailboxes: len(p.mailboxes), Campaigns: len(p.campaigns), Leads: -1, Blocklist: len(p.blocklist)}, nil
}

func (p *fakeProvider) Mailboxes(context.Context, source.Workspace) ([]source.Mailbox, error) {
	return p.mailboxes, nil
}

func (p *fakeProvider) Campaigns(context.Context, source.Workspace) ([]source.Campaign, error) {
	if p.onCampaigns != nil {
		p.onCampaigns()
	}
	return p.campaigns, nil
}

// Leads pages one lead at a time so checkpoints are exercised.
func (p *fakeProvider) Leads(ctx context.Context, _ source.Workspace, campaignID, cursor string) ([]source.Lead, string, error) {
	if p.onLeads != nil {
		if err := p.onLeads(ctx); err != nil {
			return nil, "", err
		}
	}
	list, ok := p.leads[campaignID]
	if !ok {
		return nil, "", source.ErrUnsupported
	}
	i := 0
	if cursor != "" {
		_ = json.Unmarshal([]byte(cursor), &i)
	}
	if i >= len(list) {
		return nil, "", nil
	}
	next := ""
	if i+1 < len(list) {
		raw, _ := json.Marshal(i + 1)
		next = string(raw)
	}
	return list[i : i+1], next, nil
}

func (p *fakeProvider) Blocklist(context.Context, source.Workspace, string) ([]source.BlockEntry, string, error) {
	return p.blocklist, "", nil
}

// fakeContacts imports every row as a new contact and records each call.
type fakeContacts struct {
	calls []importCall
}

type importCall struct {
	emails     []string
	campaigns  []string
	subscribed bool
	custom     map[string]string
}

func (f *fakeContacts) RunImport(ctx context.Context, _ string, _ uuid.UUID, rows []contact.ImportRow, opts *models.ContactImportCommit, sink contact.ImportSink, _ []uuid.UUID) (*models.ContactImportResult, *errx.Error) {
	call := importCall{campaigns: opts.CampaignIDs, subscribed: *opts.SubscribedDefault, custom: map[string]string{}}
	var outcomes []models.ContactImportRowOutcome
	for _, row := range rows {
		call.emails = append(call.emails, row.Cells[0])
		for _, m := range opts.Mapping {
			if m.Target == models.ContactImportTargetCustom && row.Cells[m.Index] != "" {
				call.custom[row.Cells[0]+"."+m.CustomKey] = row.Cells[m.Index]
			}
		}
		id := uuid.New()
		outcomes = append(outcomes, models.ContactImportRowOutcome{Line: row.Line, Status: models.ContactImportRowImported, Email: row.Cells[0], ContactID: &id})
	}
	f.calls = append(f.calls, call)
	_ = sink.Settle(ctx, outcomes)
	return &models.ContactImportResult{Total: len(rows), Imported: len(rows)}, nil
}

// enrolled lists every address imported into a campaign, sorted.
func (f *fakeContacts) enrolled() []string {
	var out []string
	for _, c := range f.calls {
		if len(c.campaigns) > 0 {
			out = append(out, c.emails...)
		}
	}
	sort.Strings(out)
	return out
}

type fakeCampaigns struct {
	created []*models.CreateCampaign
	ids     []uuid.UUID
}

func (f *fakeCampaigns) Create(_ context.Context, _ string, _ *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error) {
	id := uuid.New()
	f.created, f.ids = append(f.created, data), append(f.ids, id)
	return &models.Campaign{ID: id, Name: data.Name, Status: "draft"}, nil
}

func (f *fakeCampaigns) Get(_ context.Context, _, id string) (*models.Campaign, *errx.Error) {
	return &models.Campaign{ID: uuid.MustParse(id), SenderStrategy: "tags"}, nil
}

func (f *fakeCampaigns) Update(context.Context, string, string, *models.UpdateCampaign) (*models.Campaign, *errx.Error) {
	return &models.Campaign{}, nil
}

func (f *fakeCampaigns) ListCampaignSenders(context.Context, uuid.UUID, string) ([]models.CampaignSender, *errx.Error) {
	return nil, nil
}

func (f *fakeCampaigns) ReplaceCampaignSenders(context.Context, uuid.UUID, string, []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error) {
	return nil, nil
}

type fakeSteps struct{}

func (fakeSteps) Get(context.Context, string, string) ([]models.Sequence, *errx.Error) {
	return []models.Sequence{{ID: uuid.New(), Position: 1}, {ID: uuid.New(), Position: 2}}, nil
}

type fakeOutreach struct {
	suppressed []string
	variants   int
}

func (f *fakeOutreach) CreateABVariant(context.Context, uuid.UUID, *models.CreateCampaignABVariantRequest) (*models.CampaignABVariant, *errx.Error) {
	f.variants++
	return &models.CampaignABVariant{ID: uuid.New()}, nil
}

func (f *fakeOutreach) AddSuppressions(_ context.Context, _, _ uuid.UUID, req *models.AddSuppressionsRequest) (*models.AddSuppressionsResult, *errx.Error) {
	for _, e := range req.Entries {
		f.suppressed = append(f.suppressed, e.Value)
	}
	return &models.AddSuppressionsResult{Added: len(req.Entries)}, nil
}

type fakeCategories struct{}

func (fakeCategories) ResolveCategoryNames(_ context.Context, _, _ uuid.UUID, names []string) (map[string]uuid.UUID, *errx.Error) {
	out := map[string]uuid.UUID{}
	for _, n := range names {
		out[strings.ToLower(n)] = uuid.New()
	}
	return out, nil
}

// fakeMembers answers with one membership per workspace.
type fakeMembers map[uuid.UUID]*models.OrganizationMember

func (f fakeMembers) GetMembership(_ context.Context, orgID, _ uuid.UUID) (*models.OrganizationMember, *errx.Error) {
	return f[orgID], nil
}

// plainSealer is reversible but never the plaintext, which is all the tests need.
type plainSealer struct{}

func (plainSealer) Encrypt(s string) (string, error) { return "sealed:" + reverse(s), nil }
func (plainSealer) Decrypt(s string) (string, error) {
	return reverse(strings.TrimPrefix(s, "sealed:")), nil
}

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}
