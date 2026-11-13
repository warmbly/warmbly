package toolmigrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
	"github.com/warmbly/warmbly/internal/repository"
)

// Limits on what one migration may ask for.
const (
	maxActivePerScope = 5
	maxKeys           = 50
	maxCampaignIDs    = 1000
	// campaignCap bounds the campaigns one target creates; it replaces the daily creation throttle.
	campaignCap = 500
	openTimeout = 30 * time.Second
)

// Contacts runs contact imports. Satisfied by contact.ContactService.
type Contacts interface {
	RunImport(ctx context.Context, userID string, orgID uuid.UUID, rows []contact.ImportRow, opts *models.ContactImportCommit, sink contact.ImportSink, prior []uuid.UUID) (*models.ContactImportResult, *errx.Error)
}

// Campaigns creates and adjusts the draft campaigns. Satisfied by campaign.CampaignService.
type Campaigns interface {
	Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error)
	Get(ctx context.Context, orgID, id string) (*models.Campaign, *errx.Error)
	Update(ctx context.Context, orgID, id string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error)
	ListCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string) ([]models.CampaignSender, *errx.Error)
	ReplaceCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string, in []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error)
}

// Steps reads a campaign's steps. Satisfied by sequence.SequenceService.
type Steps interface {
	Get(ctx context.Context, orgID, campaignID string) ([]models.Sequence, *errx.Error)
}

// Outreach adds per-step variants and suppressions. Satisfied by advanced.Service.
type Outreach interface {
	CreateABVariant(ctx context.Context, campaignID uuid.UUID, req *models.CreateCampaignABVariantRequest) (*models.CampaignABVariant, *errx.Error)
	AddSuppressions(ctx context.Context, organizationID, actorID uuid.UUID, req *models.AddSuppressionsRequest) (*models.AddSuppressionsResult, *errx.Error)
}

// MailboxImports connects mailboxes the source returned passwords for. Satisfied by mailboximport.Service.
type MailboxImports interface {
	Create(ctx context.Context, c mailboximport.CreateInput) (*models.MailboxImport, *errx.Error)
}

// Categories finds or creates contact categories by title. Satisfied by the contact repository.
type Categories interface {
	ResolveCategoryNames(ctx context.Context, orgID, userID uuid.UUID, names []string) (map[string]uuid.UUID, *errx.Error)
}

// Members reads a membership. Satisfied by organization.OrganizationService.
type Members interface {
	GetMembership(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, *errx.Error)
}

// Agencies proves agency staff and creates client workspaces. Satisfied by agency.Service.
type Agencies interface {
	Get(ctx context.Context, agencyID, userID uuid.UUID) (*models.Agency, *errx.Error)
	CreateClient(ctx context.Context, agencyID, actorID uuid.UUID, name, timezone string) (*models.AgencyClient, *errx.Error)
}

// AgencyClients lists an agency's clients with the caller's membership. Satisfied by the agency home repository.
type AgencyClients interface {
	ClientMemberships(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyClientMembership, error)
}

// Sealer seals credentials with the instance key. Satisfied by *encrypt.Encrypter.
type Sealer interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(sealed string) (string, error)
}

// Auditor records changes on the audit spine. Satisfied by audit.AuditService.
type Auditor interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// Deps are the service's collaborators. Agencies, AgencyClients and Mailboxes are optional.
type Deps struct {
	Repo          repository.ToolMigrationRepository
	Registry      *source.Registry
	Sealer        Sealer
	Contacts      Contacts
	Campaigns     Campaigns
	Steps         Steps
	Outreach      Outreach
	Categories    Categories
	Members       Members
	Mailboxes     MailboxImports
	Agencies      Agencies
	AgencyClients AgencyClients
	Auditor       Auditor
}

// Service implements API and runs migrations in the background.
type Service struct {
	d    Deps
	kick chan struct{}
	now  func() time.Time
	// renewEvery paces lease renewal; a third of the lease leaves two chances before it lapses.
	renewEvery time.Duration
}

var _ API = (*Service)(nil)

// NewService builds the service; call Run to work migrations.
func NewService(d Deps) *Service {
	return &Service{d: d, kick: make(chan struct{}, 1), now: time.Now, renewEvery: leaseDuration / 3}
}

// WireMailboxes adds the mailbox importer, which is built later in boot.
func (s *Service) WireMailboxes(m MailboxImports) { s.d.Mailboxes = m }

func (s *Service) Providers() []source.Info {
	if s.d.Registry == nil {
		return []source.Info{}
	}
	return s.d.Registry.Infos()
}

func repoScope(scope Scope) (repository.ToolMigrationScope, *errx.Error) {
	if (scope.OrgID == nil) == (scope.AgencyID == nil) {
		return repository.ToolMigrationScope{}, errScope
	}
	return repository.ToolMigrationScope{OrgID: scope.OrgID, AgencyID: scope.AgencyID}, nil
}

// authorize proves the caller may manage migrations in the scope.
func (s *Service) authorize(ctx context.Context, scope Scope, actor Actor, manage bool) *errx.Error {
	if scope.AgencyID != nil {
		if s.d.Agencies == nil {
			return errx.New(errx.ServiceUnavailable, "agencies are not available on this instance")
		}
		a, xerr := s.d.Agencies.Get(ctx, *scope.AgencyID, actor.UserID)
		if xerr != nil {
			return xerr
		}
		if manage && !a.Role.CanManage() {
			return errPermission("Only the agency's owner and admins can migrate client accounts.")
		}
		return nil
	}
	if !actor.OrgPerms.HasPermission(models.PermManageContacts) {
		return errPermission("Migrating from another tool needs permission to manage contacts.")
	}
	return nil
}

func (s *Service) Create(ctx context.Context, scope Scope, actor Actor, in models.NewToolMigration) (*models.ToolMigration, *errx.Error) {
	rs, xerr := repoScope(scope)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.authorize(ctx, scope, actor, true); xerr != nil {
		return nil, xerr
	}
	if s.d.Registry == nil {
		return nil, errProviderUnknown
	}
	p, ok := s.d.Registry.Get(in.Provider)
	if !ok {
		return nil, errProviderUnknown
	}
	info := p.Info()
	label, xerr := displayname.Validate("Label", in.Label, displayname.Heading, true)
	if xerr != nil {
		return nil, xerr
	}
	if label == "" {
		label = info.Label
	}
	creds, xerr := cleanCredentials(info, in.Credentials)
	if xerr != nil {
		return nil, xerr
	}

	active, err := s.d.Repo.CountActive(ctx, rs)
	if err != nil {
		return nil, errx.InternalError()
	}
	if active >= maxActivePerScope {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeTooMany, fmt.Sprintf(
			"There are already %d migrations in progress here. Finish or cancel one first.", active))
	}

	accounts := make([]source.Account, 0, len(creds))
	for i, c := range creds {
		acct, xerr := checkKey(ctx, p, c, i, len(creds))
		if xerr != nil {
			return nil, xerr
		}
		accounts = append(accounts, acct)
	}

	raw, err := json.Marshal(creds)
	if err != nil {
		return nil, errx.InternalError()
	}
	sealed, err := s.d.Sealer.Encrypt(string(raw))
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	creator := actor.UserID
	m := &models.ToolMigration{
		ID: uuid.New(), OrganizationID: scope.OrgID, AgencyID: scope.AgencyID, CreatedByUserID: &creator,
		Provider: info.ID, Label: label, Status: models.ToolMigrationScanning, Accounts: accounts,
	}
	if err := s.d.Repo.Create(ctx, m, sealed, actor.ViaSession); err != nil {
		return nil, errx.InternalError()
	}
	if scope.OrgID != nil {
		s.audit(ctx, *scope.OrgID, actor.UserID, models.AuditActionCreate, m.ID, map[string]string{"provider": string(info.ID)})
	}
	s.Kick()
	return s.Get(ctx, scope, m.ID)
}

// cleanCredentials keeps each set's known fields, trimmed, and refuses a missing or unknown value.
func cleanCredentials(info source.Info, in []source.Credentials) ([]source.Credentials, *errx.Error) {
	if len(in) == 0 {
		return nil, errx.New(errx.BadRequest, "add the key for "+info.Label)
	}
	if len(in) > maxKeys {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d keys per migration", maxKeys))
	}
	if len(in) > 1 && !info.MultiKey {
		return nil, errx.New(errx.BadRequest, info.Label+" takes one key per migration")
	}
	out := make([]source.Credentials, len(in))
	for i, set := range in {
		clean := source.Credentials{}
		for _, f := range info.Fields {
			v := strings.TrimSpace(set[f.Key])
			if v == "" {
				if !f.Optional {
					return nil, errx.New(errx.BadRequest, fmt.Sprintf("%s is required", f.Label))
				}
				continue
			}
			if len(v) > 4096 {
				return nil, errx.New(errx.BadRequest, fmt.Sprintf("%s is too long", f.Label))
			}
			if len(f.Options) > 0 && !slices.Contains(f.Options, v) {
				return nil, errx.New(errx.BadRequest, fmt.Sprintf("%s must be one of %s", f.Label, strings.Join(f.Options, ", ")))
			}
			clean[f.Key] = v
		}
		out[i] = clean
	}
	return out, nil
}

// checkKey opens one credential set and reads who it acts as.
func checkKey(ctx context.Context, p source.Provider, creds source.Credentials, index, total int) (source.Account, *errx.Error) {
	cctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()
	sess, err := p.Open(cctx, creds)
	if err == nil {
		var acct source.Account
		if acct, err = sess.Account(cctx); err == nil {
			return acct, nil
		}
	}
	which := ""
	if total > 1 {
		which = "Key " + strconv.Itoa(index+1) + ": "
	}
	return source.Account{}, providerError(which, err)
}

// providerError turns a provider failure into the caller's error; provider messages are written for a person.
func providerError(prefix string, err error) *errx.Error {
	switch {
	case errors.Is(err, source.ErrKey), errors.Is(err, source.ErrForbidden):
		return errx.NewWithIdentifier(errx.Unprocessable, CodeKeyRejected, prefix+err.Error())
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, source.ErrUnavailable), errors.Is(err, source.ErrRateLimited):
		return errx.NewWithIdentifier(errx.ServiceUnavailable, CodeProviderUnavailable, prefix+"the other tool did not answer in time. Try again in a few minutes.")
	default:
		var apiErr *source.APIError
		if errors.As(err, &apiErr) {
			return errx.NewWithIdentifier(errx.Unprocessable, CodeKeyRejected, prefix+apiErr.Error())
		}
		errs.CaptureException(err)
		return errx.NewWithIdentifier(errx.ServiceUnavailable, CodeProviderUnavailable, prefix+"the other tool could not be read. Try again in a few minutes.")
	}
}

func (s *Service) List(ctx context.Context, scope Scope, cursor string, limit int) (*models.ToolMigrationList, *errx.Error) {
	rs, xerr := repoScope(scope)
	if xerr != nil {
		return nil, xerr
	}
	if limit < 1 || limit > 100 {
		return nil, errx.New(errx.BadRequest, "limit must be between 1 and 100")
	}
	var before *time.Time
	var beforeID *uuid.UUID
	if cursor != "" {
		t, id, ok := decodeCursor(cursor)
		if !ok {
			return nil, errx.New(errx.BadRequest, "invalid cursor")
		}
		before, beforeID = &t, &id
	}
	items, err := s.d.Repo.List(ctx, rs, before, beforeID, limit+1)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := &models.ToolMigrationList{Data: items}
	if len(items) > limit {
		out.Data = items[:limit]
		last := out.Data[limit-1]
		next := encodeCursor(last.CreatedAt, last.ID)
		out.Pagination = models.Pagination{NextCursor: &next, HasMore: true}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, scope Scope, id uuid.UUID) (*models.ToolMigration, *errx.Error) {
	rs, xerr := repoScope(scope)
	if xerr != nil {
		return nil, xerr
	}
	m, err := s.d.Repo.Get(ctx, rs, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil {
		return nil, errNotFound
	}
	return m, nil
}

func (s *Service) Start(ctx context.Context, scope Scope, actor Actor, id uuid.UUID, in models.StartToolMigration) (*models.ToolMigration, *errx.Error) {
	rs, xerr := repoScope(scope)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.authorize(ctx, scope, actor, true); xerr != nil {
		return nil, xerr
	}
	m, xerr := s.Get(ctx, scope, id)
	if xerr != nil {
		return nil, xerr
	}
	switch m.Status {
	case models.ToolMigrationReady:
	case models.ToolMigrationRunning:
		// A retried start answers with the migration as it stands.
		return m, nil
	default:
		return nil, errNotReady
	}
	p, ok := s.d.Registry.Get(m.Provider)
	if !ok {
		return nil, errProviderUnknown
	}
	opts, xerr := cleanOptions(p.Info(), in.Options)
	if xerr != nil {
		return nil, xerr
	}
	need := neededPerms(opts.Kinds)

	// Everything is checked before anything is written.
	var plan []plannedTarget
	if scope.OrgID != nil {
		for _, perm := range need {
			if !actor.OrgPerms.HasPermission(perm.perm) {
				return nil, errPermission(perm.msg)
			}
		}
		plan, xerr = workspaceTargets(*scope.OrgID, m.Inventory, in.Targets)
	} else {
		plan, xerr = s.agencyTargets(ctx, *scope.AgencyID, actor, m.Inventory, in.Targets, need)
	}
	if xerr != nil {
		return nil, xerr
	}
	if len(plan) == 0 {
		return nil, errTarget("Choose at least one workspace to bring over.")
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return nil, errx.InternalError()
	}

	// Only the caller holding the start creates client workspaces, and a retry reuses the ones it made.
	claimed, created, err := s.d.Repo.ClaimStart(ctx, rs, id, startHold)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !claimed {
		return s.notStarted(ctx, scope, id)
	}
	targets, xerr := s.materialize(ctx, id, *m, actor, plan, created, need)
	if xerr == nil {
		var started bool
		if started, err = s.d.Repo.Start(ctx, rs, id, raw, targets); err != nil {
			xerr = errx.InternalError()
		} else if !started {
			xerr = errNotReady
		}
	}
	if xerr != nil {
		if err := s.d.Repo.ReleaseStart(context.WithoutCancel(ctx), id); err != nil {
			errs.CaptureException(err)
		}
		if xerr == errNotReady {
			return s.notStarted(ctx, scope, id)
		}
		return nil, xerr
	}
	seen := map[uuid.UUID]bool{}
	for _, t := range targets {
		if !seen[t.OrganizationID] {
			seen[t.OrganizationID] = true
			s.audit(ctx, t.OrganizationID, actor.UserID, models.AuditActionStart, id, map[string]string{"provider": string(m.Provider)})
		}
	}
	s.Kick()
	return s.Get(ctx, scope, id)
}

// startHold bounds how long one start may hold a migration before another can try.
const startHold = 2 * time.Minute

var errStarting = errx.NewWithIdentifier(errx.Conflict, CodeRunning, "This migration is being started. Try again in a moment.")

// notStarted answers a start that did not go through: the migration as it now runs, or why not.
func (s *Service) notStarted(ctx context.Context, scope Scope, id uuid.UUID) (*models.ToolMigration, *errx.Error) {
	cur, xerr := s.Get(ctx, scope, id)
	if xerr != nil {
		return nil, xerr
	}
	switch cur.Status {
	case models.ToolMigrationRunning:
		return cur, nil
	case models.ToolMigrationReady:
		return nil, errStarting
	}
	return nil, errNotReady
}

// plannedTarget is a checked target; create names a client workspace still to be made.
type plannedTarget struct {
	ws     source.Workspace
	orgID  uuid.UUID
	create string
}

// materialize creates the planned clients, reusing any an earlier attempt recorded, and returns the targets.
func (s *Service) materialize(ctx context.Context, id uuid.UUID, m models.ToolMigration, actor Actor, plan []plannedTarget, created map[string]uuid.UUID, need []permNeed) ([]repository.ToolMigrationTargetInsert, *errx.Error) {
	out := make([]repository.ToolMigrationTargetInsert, 0, len(plan))
	for _, p := range plan {
		orgID := p.orgID
		if p.create != "" {
			key := choiceKey(p.ws.ID, p.ws.Key)
			if made, ok := created[key]; ok {
				orgID = made
			} else {
				client, xerr := s.d.Agencies.CreateClient(ctx, *m.AgencyID, actor.UserID, p.create, "")
				if xerr != nil {
					return nil, xerr
				}
				orgID = client.OrganizationID
				if err := s.d.Repo.RecordClient(context.WithoutCancel(ctx), id, key, orgID); err != nil {
					return nil, errx.InternalError()
				}
			}
			if xerr := s.requireMember(ctx, orgID, actor.UserID, need); xerr != nil {
				return nil, xerr
			}
		}
		out = append(out, repository.ToolMigrationTargetInsert{
			ID: uuid.New(), OrganizationID: orgID, WorkspaceID: p.ws.ID, KeyIndex: p.ws.Key, WorkspaceName: p.ws.Name,
		})
	}
	return out, nil
}

// cleanOptions orders the chosen kinds and refuses what the provider cannot read.
func cleanOptions(info source.Info, in models.ToolMigrationOptions) (models.ToolMigrationOptions, *errx.Error) {
	out := in
	out.Kinds = nil
	for _, k := range source.AllKinds {
		if slices.Contains(in.Kinds, k) {
			if !info.Supports(k) {
				return out, errx.New(errx.BadRequest, fmt.Sprintf("%s does not share %s through its API", info.Label, k))
			}
			out.Kinds = append(out.Kinds, k)
		}
	}
	for _, k := range in.Kinds {
		if !slices.Contains(source.AllKinds, k) {
			return out, errx.New(errx.BadRequest, "unknown kind: "+string(k))
		}
	}
	if len(out.Kinds) == 0 {
		return out, errx.New(errx.BadRequest, "choose at least one kind of data to bring over")
	}
	switch in.InFlight {
	case "":
		out.InFlight = models.ToolMigrationSkip
	case models.ToolMigrationRestart, models.ToolMigrationSkip:
	case models.ToolMigrationContinue:
		return out, errInFlight
	default:
		return out, errx.New(errx.BadRequest, "in_flight must be restart or skip")
	}
	if len(in.CampaignIDs) > maxCampaignIDs {
		return out, errx.New(errx.BadRequest, fmt.Sprintf("at most %d campaigns can be chosen", maxCampaignIDs))
	}
	ids := make([]string, 0, len(in.CampaignIDs))
	for _, c := range in.CampaignIDs {
		if c = strings.TrimSpace(c); c != "" && !slices.Contains(ids, c) {
			ids = append(ids, c)
		}
	}
	out.CampaignIDs = ids
	return out, nil
}

type permNeed struct {
	perm models.OrganizationPermission
	msg  string
}

// neededPerms are the workspace permissions the chosen kinds write under.
func neededPerms(kinds []source.Kind) []permNeed {
	out := []permNeed{{models.PermManageContacts, "Migrating from another tool needs permission to manage contacts."}}
	if slices.Contains(kinds, source.KindCampaigns) {
		out = append(out, permNeed{models.PermManageCampaigns, "Bringing campaigns over needs permission to manage campaigns."})
	}
	if slices.Contains(kinds, source.KindMailboxes) {
		out = append(out, permNeed{models.PermManageEmails, "Bringing mailboxes over needs permission to manage mailboxes."})
	}
	return out
}

func choiceKey(workspaceID string, key int) string {
	return strconv.Itoa(key) + "\x1f" + workspaceID
}

// workspaceTargets lands every readable source workspace in the caller's own workspace, minus the skipped ones.
func workspaceTargets(orgID uuid.UUID, inv []models.ToolMigrationWorkspace, choices []models.ToolMigrationTargetChoice) ([]plannedTarget, *errx.Error) {
	skip := map[string]bool{}
	for _, c := range choices {
		if c.OrganizationID != nil || c.CreateName != "" {
			return nil, errTarget("A workspace's own migration brings everything into this workspace; only skip can be chosen.")
		}
		if c.Skip {
			skip[choiceKey(c.WorkspaceID, c.Key)] = true
		}
	}
	var out []plannedTarget
	for _, w := range inv {
		if w.Error != "" || skip[choiceKey(w.Workspace.ID, w.Workspace.Key)] {
			continue
		}
		out = append(out, plannedTarget{ws: w.Workspace, orgID: orgID})
	}
	return out, nil
}

// agencyTargets maps each chosen source workspace onto a client, checking every
// existing client and the caller's membership in it; it writes nothing.
func (s *Service) agencyTargets(ctx context.Context, agencyID uuid.UUID, actor Actor, inv []models.ToolMigrationWorkspace, choices []models.ToolMigrationTargetChoice, need []permNeed) ([]plannedTarget, *errx.Error) {
	if s.d.AgencyClients == nil || s.d.Members == nil {
		return nil, errx.New(errx.ServiceUnavailable, "agencies are not available on this instance")
	}
	byKey := map[string]models.ToolMigrationWorkspace{}
	for _, w := range inv {
		byKey[choiceKey(w.Workspace.ID, w.Workspace.Key)] = w
	}
	memberships, err := s.d.AgencyClients.ClientMemberships(ctx, agencyID, actor.UserID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	clients := map[uuid.UUID]models.AgencyClientMembership{}
	for _, m := range memberships {
		clients[m.OrganizationID] = m
	}

	var plan []plannedTarget
	seen := map[string]bool{}
	for _, c := range choices {
		k := choiceKey(c.WorkspaceID, c.Key)
		w, ok := byKey[k]
		if !ok {
			return nil, errTarget("A chosen workspace is not one this migration found.")
		}
		if seen[k] {
			return nil, errTarget("Each source workspace can be chosen once.")
		}
		seen[k] = true
		if c.Skip {
			continue
		}
		if w.Error != "" {
			return nil, errTarget(fmt.Sprintf("%q could not be read, so it cannot be brought over.", w.Workspace.Name))
		}
		name := strings.TrimSpace(c.CreateName)
		switch {
		case c.OrganizationID != nil && name == "":
			cl, ok := clients[*c.OrganizationID]
			if !ok {
				return nil, errTarget("That workspace is not one of this agency's clients.")
			}
			if xerr := s.requireMember(ctx, cl.OrganizationID, actor.UserID, need); xerr != nil {
				return nil, xerr
			}
			plan = append(plan, plannedTarget{ws: w.Workspace, orgID: cl.OrganizationID})
		case c.OrganizationID == nil && name != "":
			if _, xerr := displayname.Validate("Workspace name", name, displayname.Workspace, false); xerr != nil {
				return nil, xerr
			}
			plan = append(plan, plannedTarget{ws: w.Workspace, create: name})
		default:
			return nil, errTarget("Choose an existing client or a name for a new one, not both.")
		}
	}

	return plan, nil
}

// requireMember checks an accepted, workspace-wide membership holding every needed permission.
func (s *Service) requireMember(ctx context.Context, orgID, userID uuid.UUID, need []permNeed) *errx.Error {
	m, xerr := s.d.Members.GetMembership(ctx, orgID, userID)
	if xerr != nil {
		return xerr
	}
	if m == nil || (!m.IsOwner() && m.AcceptedAt == nil) {
		return errPermission("You are not a member of that client's workspace.")
	}
	if m.IsRestricted() {
		return errRestricted
	}
	for _, n := range need {
		if !m.HasPermission(n.perm) {
			return errPermission(n.msg)
		}
	}
	return nil
}

func (s *Service) Cancel(ctx context.Context, scope Scope, actor Actor, id uuid.UUID) (*models.ToolMigration, *errx.Error) {
	rs, xerr := repoScope(scope)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.authorize(ctx, scope, actor, true); xerr != nil {
		return nil, xerr
	}
	canceled, err := s.d.Repo.Cancel(ctx, rs, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	m, xerr := s.Get(ctx, scope, id)
	if xerr != nil {
		return nil, xerr
	}
	if canceled {
		for _, org := range auditOrgs(m) {
			s.audit(ctx, org, actor.UserID, models.AuditActionStop, id, map[string]string{"provider": string(m.Provider)})
		}
	}
	return m, nil
}

func (s *Service) Dismiss(ctx context.Context, scope Scope, actor Actor, id uuid.UUID) *errx.Error {
	rs, xerr := repoScope(scope)
	if xerr != nil {
		return xerr
	}
	if xerr := s.authorize(ctx, scope, actor, true); xerr != nil {
		return xerr
	}
	m, xerr := s.Get(ctx, scope, id)
	if xerr != nil {
		return xerr
	}
	if m.Status == models.ToolMigrationRunning {
		return errRunning
	}
	deleted, err := s.d.Repo.Delete(ctx, rs, id)
	if err != nil {
		return errx.InternalError()
	}
	if !deleted {
		return errRunning
	}
	for _, org := range auditOrgs(m) {
		s.audit(ctx, org, actor.UserID, models.AuditActionDelete, id, nil)
	}
	return nil
}

// auditOrgs are the workspaces a migration touches, for the audit spine.
func auditOrgs(m *models.ToolMigration) []uuid.UUID {
	var out []uuid.UUID
	if m.OrganizationID != nil {
		out = append(out, *m.OrganizationID)
	}
	for _, t := range m.Targets {
		if !slices.Contains(out, t.OrganizationID) {
			out = append(out, t.OrganizationID)
		}
	}
	return out
}

func (s *Service) audit(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, id uuid.UUID, meta map[string]string) {
	if s.d.Auditor == nil {
		return
	}
	s.d.Auditor.LogAction(ctx, orgID, actorID, action, models.AuditEntityToolMigration, &id, "", "", nil, meta)
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	return strconv.FormatInt(t.UnixMicro(), 36) + "." + id.String()
}

func decodeCursor(c string) (time.Time, uuid.UUID, bool) {
	ts, rawID, ok := strings.Cut(c, ".")
	if !ok {
		return time.Time{}, uuid.Nil, false
	}
	micros, err := strconv.ParseInt(ts, 36, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return time.UnixMicro(micros).UTC(), id, true
}
