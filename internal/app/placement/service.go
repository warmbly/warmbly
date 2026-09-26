// Package placement runs inbox placement tests: a template or a campaign step
// is sent from a real sender mailbox to a panel of seed mailboxes, each copy
// rendered the way the campaign would render it and paced as its own task, and
// where each copy landed (inbox, a Gmail tab, spam, never arrived) is read
// back from the seed's synced mail by Message-ID.
//
// Three panels exist: the instance panel the operator runs for everyone, a
// workspace's own test inboxes, and Warmbly Cloud's panel for a linked
// self-hosted instance.
package placement

import (
	"context"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	"github.com/warmbly/warmbly/internal/tasksched"
)

// Policy is the operator's placement section, satisfied by
// instancesettings.Service.
type Policy interface {
	PlacementPolicy(ctx context.Context) instancesettings.Placement
}

// Entitlements answers who may test and on which allowance. feature.Gate
// satisfies it.
type Entitlements interface {
	CanSendCampaignEmail(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
	IsPaidOrganization(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// Publisher tells the dashboard a test moved. pubsub.StreamingPublisher
// satisfies it.
type Publisher interface {
	PublishPlacementTest(ctx context.Context, orgID, testID uuid.UUID, campaignID *uuid.UUID, status string)
}

// Notifier reaches people about a finished test or a monitor alert.
type Notifier interface {
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)
	NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)
}

// MailboxControl is what a seed toggle needs from the mailbox service: stop
// its warmup and reload it onto its worker with the seed sync allowance.
type MailboxControl interface {
	SetWarmupLifecycle(ctx context.Context, orgID, emailAccountID, action string) (*models.Email, *errx.Error)
	LoadAccountOntoWorker(ctx context.Context, accountID uuid.UUID) error
}

// CampaignPauser pauses a campaign whose monitor alerted, when asked to.
type CampaignPauser interface {
	PausePlacementAlert(ctx context.Context, orgID, campaignID uuid.UUID, reason string) error
}

// CloudPanel is the instance side of Warmbly Cloud's seed panel. The
// cloudlink service satisfies it; nil when the instance is not linked.
type CloudPanel interface {
	PlacementPanel(ctx context.Context) (*models.PlacementCloudPanel, *errx.Error)
	StartPlacement(ctx context.Context, req models.PlacementCloudStartRequest) (*models.PlacementCloudStart, *errx.Error)
	ReportPlacementSends(ctx context.Context, testID uuid.UUID, sends []models.PlacementCloudSend) *errx.Error
	PlacementVerdicts(ctx context.Context, testID uuid.UUID) (*models.PlacementCloudTest, *errx.Error)
}

// Deps are the service's collaborators. Repo, Emails, Campaigns, Tasks and
// Scheduler are required; the rest are optional.
type Deps struct {
	Repo      repository.PlacementRepository
	Emails    repository.EmailRepository
	Campaigns repository.CampaignRepository
	Contacts  repository.ContactRepository
	Tasks     repository.TaskRepository
	Scheduler tasksched.Scheduler
	Policy    Policy
	Gate      Entitlements
	Publisher Publisher
	Notifier  Notifier
	Mailboxes MailboxControl
	Pauser    CampaignPauser
	Cloud     CloudPanel
}

// Service is the placement test surface.
type Service interface {
	Overview(ctx context.Context, orgID uuid.UUID) (*Overview, *errx.Error)
	CreateTests(ctx context.Context, in CreateInput) ([]TestView, *errx.Error)
	ListTests(ctx context.Context, orgID *uuid.UUID, campaignID *uuid.UUID, limit, offset int) ([]TestView, int, *errx.Error)
	GetTest(ctx context.Context, orgID *uuid.UUID, id uuid.UUID) (*TestDetail, *errx.Error)
	CancelTest(ctx context.Context, orgID, id uuid.UUID) (*TestView, *errx.Error)

	ListWorkspaceSeeds(ctx context.Context, orgID uuid.UUID) ([]models.PlacementWorkspaceSeed, *errx.Error)
	SetWorkspaceSeed(ctx context.Context, orgID, accountID uuid.UUID, enabled bool) (*models.PlacementWorkspaceSeed, *errx.Error)

	GetMonitor(ctx context.Context, orgID, campaignID uuid.UUID) (*models.PlacementMonitor, *errx.Error)
	PutMonitor(ctx context.Context, orgID, userID, campaignID uuid.UUID, in MonitorInput) (*models.PlacementMonitor, *errx.Error)
	DeleteMonitor(ctx context.Context, orgID, campaignID uuid.UUID) *errx.Error

	AdminListSeeds(ctx context.Context) ([]AdminSeed, *errx.Error)
	AdminSeedCandidates(ctx context.Context, search string, limit int) ([]AdminSeed, *errx.Error)
	AdminSetSeed(ctx context.Context, accountID uuid.UUID, enabled bool) (*AdminSeed, *errx.Error)

	RemotePanel(ctx context.Context, inst *models.PoolLinkInstance) (*models.PlacementCloudPanel, *errx.Error)
	RemoteStart(ctx context.Context, inst *models.PoolLinkInstance, req models.PlacementCloudStartRequest) (*models.PlacementCloudStart, *errx.Error)
	RemoteSends(ctx context.Context, inst *models.PoolLinkInstance, testID uuid.UUID, sends []models.PlacementCloudSend) *errx.Error
	RemoteGet(ctx context.Context, inst *models.PoolLinkInstance, testID uuid.UUID) (*models.PlacementCloudTest, *errx.Error)

	// Tick classifies delivered probes, closes finished tests, syncs cloud
	// tests and runs due monitors. The poller calls it.
	Tick(ctx context.Context) error
}

type service struct {
	Deps
	now func() time.Time
}

// NewService wires the placement service.
func NewService(d Deps) Service {
	return &service{Deps: d, now: time.Now}
}

// CreateInput is one request to test a template.
type CreateInput struct {
	OrgID           uuid.UUID
	UserID          *uuid.UUID
	SenderAccountID uuid.UUID
	CampaignID      *uuid.UUID
	SequenceID      *uuid.UUID
	ContactID       *uuid.UUID
	Subject         string
	BodyHTML        string
	BodyPlain       string
	Tracking        string
	Panel           string
	Origin          string
	MonitorID       *uuid.UUID
}

func (s *service) policy(ctx context.Context) instancesettings.Placement {
	if s.Policy == nil {
		p := instancesettings.DefaultPlacement()
		return p
	}
	return s.Policy.PlacementPolicy(ctx)
}

func placementErr(code errx.Code, id, msg string) *errx.Error {
	return errx.NewWithIdentifier(code, id, msg)
}

// CreateTests validates a request, picks the seeds, writes the test (two for
// a tracking comparison) with one task per probe, and schedules the tasks.
func (s *service) CreateTests(ctx context.Context, in CreateInput) ([]TestView, *errx.Error) {
	if in.Panel == "" {
		in.Panel = models.PlacementPanelInstance
	}
	if !models.ValidPlacementPanel(in.Panel) {
		return nil, errx.New(errx.BadRequest, "panel must be instance, workspace or cloud")
	}
	if in.Origin == "" {
		in.Origin = models.PlacementOriginManual
	}
	if in.Tracking == "" {
		in.Tracking = models.PlacementTrackingCampaign
	}
	switch in.Tracking {
	case models.PlacementTrackingCampaign, models.PlacementTrackingOn, models.PlacementTrackingOff, models.PlacementTrackingCompare:
	default:
		return nil, errx.New(errx.BadRequest, "tracking must be campaign, on, off or compare")
	}

	if s.Gate != nil && !config.SelfHosted() {
		if ok, _ := s.Gate.CanSendCampaignEmail(ctx, in.OrgID); !ok {
			return nil, placementErr(errx.PaymentRequired, "placement_not_entitled", "Placement tests need an active trial or subscription.")
		}
	}

	// The sender: an active mailbox of this workspace, on a worker, not itself
	// a seed.
	sender, xerr := s.Emails.GetByID(ctx, in.SenderAccountID)
	if xerr != nil || sender == nil || sender.OrganizationID == nil || *sender.OrganizationID != in.OrgID {
		return nil, errx.New(errx.NotFound, "sending mailbox not found")
	}
	if sender.Status != "active" || sender.WorkerID == nil {
		return nil, placementErr(errx.Conflict, "placement_sender_unavailable", "The sending mailbox is not connected and running, so it cannot send a test.")
	}
	if scope, err := s.Repo.SeedScope(ctx, sender.ID); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	} else if scope != "" {
		return nil, placementErr(errx.Conflict, "placement_sender_unavailable", "A seed mailbox receives tests; it cannot send one.")
	}

	// The copy: a campaign step (snapshotted now, so an edit mid-test does not
	// change what the later seeds get) or an ad-hoc template.
	var campaign *models.Campaign
	if in.CampaignID != nil {
		c, err := s.Campaigns.GetByID(ctx, *in.CampaignID)
		if err != nil || c == nil || c.OrganizationID == nil || *c.OrganizationID != in.OrgID {
			return nil, errx.New(errx.NotFound, "campaign not found")
		}
		campaign = c
		if in.SequenceID != nil {
			seq := s.campaignStep(ctx, c.ID, *in.SequenceID)
			if seq == nil {
				return nil, errx.New(errx.NotFound, "campaign step not found")
			}
			if strings.TrimSpace(in.Subject) == "" && in.BodyHTML == "" && in.BodyPlain == "" {
				in.Subject, in.BodyHTML, in.BodyPlain = seq.Subject, seq.BodyHTML, seq.BodyPlain
			}
			if strings.TrimSpace(in.Subject) == "" {
				in.Subject = s.threadSubject(ctx, c.ID, seq)
			}
		}
	} else if in.SequenceID != nil {
		return nil, errx.New(errx.BadRequest, "sequence_id needs campaign_id")
	}
	// A campaign test renders for the campaign's first lead unless told
	// otherwise, so merge fields and AI blocks read as a lead would get them.
	if in.ContactID == nil && campaign != nil {
		if lead, err := s.Repo.SampleLead(ctx, campaign.ID); err == nil {
			in.ContactID = lead
		}
	}
	if in.ContactID != nil {
		found, xerr := s.contactsInOrg(ctx, in.OrgID, *in.ContactID)
		if xerr != nil {
			return nil, xerr
		}
		if !found {
			return nil, errx.New(errx.NotFound, "contact not found")
		}
	}
	in.Subject = strings.TrimSpace(in.Subject)
	if in.Subject == "" {
		return nil, errx.New(errx.BadRequest, "subject is required")
	}
	if !mailhtml.HasContent(in.BodyHTML) && strings.TrimSpace(in.BodyPlain) == "" {
		return nil, errx.New(errx.BadRequest, "a plain-text or HTML body is required")
	}
	if len(in.Subject) > config.SequenceSubjectLimit*4 || len(in.BodyHTML)+len(in.BodyPlain) > config.SequenceBodyLimit*4 {
		return nil, errx.New(errx.BadRequest, "the template is too long")
	}

	// Tracking, resolved to what each test's copies carry.
	textOnly := campaign != nil && campaign.TextOnly
	campOpen, campLink := false, false
	if campaign != nil {
		campOpen, campLink = campaign.OpenTracking, campaign.LinkTracking
	}
	type variant struct{ open, link bool }
	var variants []variant
	switch in.Tracking {
	case models.PlacementTrackingCampaign:
		variants = []variant{{campOpen, campLink}}
	case models.PlacementTrackingOn:
		variants = []variant{{true, true}}
	case models.PlacementTrackingOff:
		variants = []variant{{false, false}}
	case models.PlacementTrackingCompare:
		tracked := variant{campOpen, campLink}
		if !tracked.open && !tracked.link {
			tracked = variant{true, true}
		}
		variants = []variant{{false, false}, tracked}
	}
	if textOnly && (in.Tracking == models.PlacementTrackingOn || in.Tracking == models.PlacementTrackingCompare) {
		return nil, placementErr(errx.BadRequest, "placement_invalid_tracking", "This campaign sends plain text, which carries no tracking to compare.")
	}
	if textOnly {
		variants = []variant{{false, false}}
	}

	// Limits: tests in flight, the sender's own queue, the monthly allowance.
	if in.Origin == models.PlacementOriginManual || in.Origin == models.PlacementOriginMonitor {
		running, err := s.Repo.CountRunning(ctx, in.OrgID)
		if err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		if running+len(variants) > config.PlacementRunningPerOrgMax {
			return nil, placementErr(errx.TooManyRequests, "placement_too_many_running",
				"This workspace already has placement tests running. Wait for one to finish.")
		}
	}
	if busy, err := s.Repo.SenderBusy(ctx, sender.ID); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	} else if busy {
		return nil, placementErr(errx.Conflict, "placement_sender_busy", "A placement test is still sending from this mailbox.")
	}
	metered := in.Panel == models.PlacementPanelInstance && in.Origin != models.PlacementOriginAdmin
	if metered {
		usage, xerr := s.usage(ctx, in.OrgID)
		if xerr != nil {
			return nil, xerr
		}
		if usage.Limit != nil && usage.Used+len(variants) > *usage.Limit {
			return nil, placementErr(errx.PaymentRequired, "placement_quota_exceeded",
				"This workspace has used its placement tests for the month.")
		}
	}

	pol := s.policy(ctx)
	senderDomain := domainOf(sender.SendFrom())

	// Seeds, per panel.
	var seeds []models.PlacementSeed
	var cloudStart *models.PlacementCloudStart
	switch in.Panel {
	case models.PlacementPanelInstance, models.PlacementPanelWorkspace:
		scope, orgFilter := models.SeedScopeInstance, (*uuid.UUID)(nil)
		if in.Panel == models.PlacementPanelWorkspace {
			scope, orgFilter = models.SeedScopeWorkspace, &in.OrgID
		}
		rows, err := s.Repo.ListSeeds(ctx, scope, orgFilter, true)
		if err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		seeds = pickSeeds(rows, sender.ID, senderDomain, pol.SeedsPerTest)
	case models.PlacementPanelCloud:
		if s.Cloud == nil {
			return nil, placementErr(errx.Conflict, "placement_panel_unavailable", "Link this instance to Warmbly Cloud to test on its seed panel.")
		}
		// The cloud charges its allowance when it opens the test, so what can
		// be refused here is refused before that.
		if panel, xerr := s.Cloud.PlacementPanel(ctx); xerr == nil && panel != nil {
			if lim := panel.Usage.Limit; lim != nil && panel.Usage.Used+len(variants) > *lim {
				return nil, placementErr(errx.PaymentRequired, "placement_quota_exceeded",
					"The linked Warmbly Cloud workspace has used its placement tests for the month.")
			}
			if xerr := s.checkBudget(ctx, sender, len(variants)*min(panel.Panel.Seeds, pol.SeedsPerTest)); xerr != nil {
				return nil, xerr
			}
		}
		start, xerr := s.Cloud.StartPlacement(ctx, models.PlacementCloudStartRequest{SenderDomain: senderDomain, Tests: len(variants)})
		if xerr != nil {
			return nil, xerr
		}
		if len(start.TestIDs) != len(variants) {
			return nil, errx.New(errx.ServiceUnavailable, "Warmbly Cloud did not open the test")
		}
		cloudStart = start
		for _, cs := range start.Seeds {
			id := cs.ID
			seeds = append(seeds, models.PlacementSeed{RemoteSeedID: &id, Address: cs.Address, Family: cs.Family})
		}
	}
	if len(seeds) == 0 {
		return nil, placementErr(errx.Conflict, "placement_no_seeds", noSeedsMessage(in.Panel))
	}

	// Budget: every probe is a send from the sender's day.
	if xerr := s.checkBudget(ctx, sender, len(seeds)*len(variants)); xerr != nil {
		return nil, xerr
	}

	// Build the tests, their probes and one task per probe.
	now := s.now()
	var group *uuid.UUID
	if len(variants) > 1 {
		g := uuid.New()
		group = &g
	}
	type built struct {
		test    models.PlacementTest
		results []models.PlacementResult
		tasks   []repository.Task
	}
	out := make([]built, len(variants))
	for i, v := range variants {
		senderID := sender.ID
		out[i].test = models.PlacementTest{
			ID:              uuid.New(),
			OrganizationID:  &in.OrgID,
			SenderAccountID: &senderID,
			SenderEmail:     sender.SendFrom(),
			CreatedBy:       in.UserID,
			CampaignID:      in.CampaignID,
			SequenceID:      in.SequenceID,
			ContactID:       in.ContactID,
			MonitorID:       in.MonitorID,
			Subject:         in.Subject,
			BodyHTML:        in.BodyHTML,
			BodyPlain:       in.BodyPlain,
			OpenTracking:    v.open,
			LinkTracking:    v.link,
			CompareGroupID:  group,
			Origin:          in.Origin,
			Panel:           in.Panel,
			Status:          models.PlacementStatusRunning,
		}
		if cloudStart != nil {
			remote := cloudStart.TestIDs[i]
			out[i].test.RemoteTestID = &remote
		}
	}
	// One probe every spacing, jittered, alternating variants per seed in a
	// random order so both see the same conditions.
	at := now.Add(5 * time.Second)
	spacing := pol.Spacing()
	for _, seed := range seeds {
		order := rand.Perm(len(variants))
		for _, vi := range order {
			taskID := uuid.New()
			slot := at
			out[vi].tasks = append(out[vi].tasks, repository.Task{
				ID: taskID, TaskType: "placement", EmailAccountID: sender.ID, Status: "pending", ScheduledAt: &slot,
			})
			seed := seed
			out[vi].results = append(out[vi].results, models.PlacementResult{
				ID:            uuid.New(),
				SeedAccountID: seed.AccountID,
				SeedAddress:   seed.Address,
				Family:        seed.Family,
				RemoteSeedID:  seed.RemoteSeedID,
				TaskID:        &taskID,
				Folder:        models.PlacementFolderPending,
				ScheduledAt:   &slot,
			})
			at = at.Add(jitter(spacing))
		}
	}

	views := make([]TestView, 0, len(out))
	for i := range out {
		b := &out[i]
		if err := s.Repo.CreateTest(ctx, &b.test, b.results, b.tasks); err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		s.enqueue(ctx, b.tasks, b.results)
		s.publish(ctx, &b.test)
		views = append(views, s.view(b.test, b.results, false))
	}
	return views, nil
}

// checkBudget refuses a test whose copies do not fit in what is left of the
// sender's daily campaign limit.
func (s *service) checkBudget(ctx context.Context, sender *models.Email, probes int) *errx.Error {
	if s.Tasks == nil {
		return nil
	}
	sent, err := s.Tasks.CountCampaignEmailsSentToday(ctx, sender.ID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if left := sender.CampaignLimit - sent; probes > left {
		return placementErr(errx.Conflict, "placement_daily_budget",
			"This test sends "+strconv.Itoa(probes)+" emails from "+sender.Email+", which has "+strconv.Itoa(max(0, left))+
				" left of its daily limit today.")
	}
	return nil
}

// enqueue hands each probe's task to the scheduler. The local scheduler picks
// due pending rows on its own; Cloud Tasks needs the handle stored.
func (s *service) enqueue(ctx context.Context, tasks []repository.Task, results []models.PlacementResult) {
	if s.Scheduler == nil {
		return
	}
	for i, t := range tasks {
		name, err := s.Scheduler.CreateTask(ctx, &proto.ProcessTask{TaskId: t.ID.String()}, *t.ScheduledAt)
		if err != nil {
			errs.CaptureException(err)
			_ = s.Repo.FailProbe(ctx, results[i].ID, "The send could not be scheduled")
			continue
		}
		if name != "" && s.Tasks != nil {
			_ = s.Tasks.UpdateTaskScheduledAt(ctx, t.ID, *t.ScheduledAt, name)
		}
	}
}

func (s *service) publish(ctx context.Context, t *models.PlacementTest) {
	if s.Publisher == nil || t.OrganizationID == nil {
		return
	}
	s.Publisher.PublishPlacementTest(ctx, *t.OrganizationID, t.ID, t.CampaignID, t.Status)
}

// threadSubject is the subject a threading follow-up inherits: the subject of
// the closest earlier step that has one. A probe is a standalone message, so
// it goes without the "Re:" a real reply would carry.
func (s *service) threadSubject(ctx context.Context, campaignID uuid.UUID, step *models.Sequence) string {
	seqs, err := s.Campaigns.GetSequencesByCampaignID(ctx, campaignID)
	if err != nil {
		return ""
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i].Position < seqs[j].Position })
	subject := ""
	for _, q := range seqs {
		if q.Position >= step.Position {
			break
		}
		if strings.TrimSpace(q.Subject) != "" {
			subject = q.Subject
		}
	}
	return subject
}

// campaignStep is one step of a campaign, nil when the id names another
// campaign's step.
func (s *service) campaignStep(ctx context.Context, campaignID, sequenceID uuid.UUID) *models.Sequence {
	seqs, err := s.Campaigns.GetSequencesByCampaignID(ctx, campaignID)
	if err != nil {
		return nil
	}
	for i := range seqs {
		if seqs[i].ID == sequenceID {
			return &seqs[i]
		}
	}
	return nil
}

func (s *service) contactsInOrg(ctx context.Context, orgID, contactID uuid.UUID) (bool, *errx.Error) {
	if s.Contacts == nil {
		return false, nil
	}
	found, xerr := s.Contacts.GetByIDsAndOrganization(ctx, orgID, []uuid.UUID{contactID})
	if xerr != nil {
		return false, xerr
	}
	return len(found) == 1, nil
}

// pickSeeds chooses up to n seeds, never the sender and never one on the
// sender's domain, round-robin across host families so a small test still
// covers every provider.
func pickSeeds(rows []repository.SeedAccount, senderID uuid.UUID, senderDomain string, n int) []models.PlacementSeed {
	byFamily := map[string][]models.PlacementSeed{}
	var families []string
	for _, r := range rows {
		if r.ID == senderID || strings.EqualFold(domainOf(r.Email), senderDomain) {
			continue
		}
		fam := string(mailhost.ForMailbox(r.MailHost, r.Provider, r.Email))
		if _, ok := byFamily[fam]; !ok {
			families = append(families, fam)
		}
		id := r.ID
		byFamily[fam] = append(byFamily[fam], models.PlacementSeed{AccountID: &id, Address: r.Email, Family: fam, OrganizationID: r.OrganizationID})
	}
	sort.Strings(families)
	for _, f := range families {
		rand.Shuffle(len(byFamily[f]), func(i, j int) { byFamily[f][i], byFamily[f][j] = byFamily[f][j], byFamily[f][i] })
	}
	var out []models.PlacementSeed
	for len(out) < n {
		added := false
		for _, f := range families {
			if len(out) >= n {
				break
			}
			if len(byFamily[f]) == 0 {
				continue
			}
			out = append(out, byFamily[f][0])
			byFamily[f] = byFamily[f][1:]
			added = true
		}
		if !added {
			break
		}
	}
	return out
}

func noSeedsMessage(panel string) string {
	switch panel {
	case models.PlacementPanelWorkspace:
		return "Add a test inbox under Placement tests > Seed inboxes first. It has to be on a different domain from the sender."
	case models.PlacementPanelCloud:
		return "Warmbly Cloud has no seed inboxes available for this sender right now."
	}
	return "This instance has no seed inboxes on a different domain from the sender yet."
}

// jitter spreads a gap by up to a third either way.
func jitter(d time.Duration) time.Duration {
	third := int64(d) / 3
	if third <= 0 {
		return d
	}
	return time.Duration(int64(d) - third + rand.Int64N(2*third+1))
}

func domainOf(address string) string {
	if at := strings.LastIndex(address, "@"); at >= 0 && at < len(address)-1 {
		return strings.ToLower(strings.TrimSpace(address[at+1:]))
	}
	return ""
}

// CancelTest stops the probes of a running test that have not left yet.
func (s *service) CancelTest(ctx context.Context, orgID, id uuid.UUID) (*TestView, *errx.Error) {
	ok, err := s.Repo.CancelTest(ctx, orgID, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	test, err := s.Repo.GetOrgTest(ctx, orgID, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if test == nil {
		return nil, errx.New(errx.NotFound, "placement test not found")
	}
	if !ok {
		return nil, placementErr(errx.Conflict, "placement_not_running", "This placement test is no longer running.")
	}
	results, err := s.Repo.ListResults(ctx, []uuid.UUID{id})
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	s.publish(ctx, test)
	v := s.view(*test, results[id], false)
	return &v, nil
}

// ListTests lists tests newest first; a nil orgID lists every workspace's.
func (s *service) ListTests(ctx context.Context, orgID *uuid.UUID, campaignID *uuid.UUID, limit, offset int) ([]TestView, int, *errx.Error) {
	tests, total, err := s.Repo.ListTests(ctx, repository.PlacementTestFilter{
		OrganizationID: orgID, CampaignID: campaignID, Limit: limit, Offset: offset,
	})
	if err != nil {
		errs.CaptureException(err)
		return nil, 0, errx.InternalError()
	}
	ids := make([]uuid.UUID, len(tests))
	for i, t := range tests {
		ids[i] = t.ID
	}
	results, err := s.Repo.ListResults(ctx, ids)
	if err != nil {
		errs.CaptureException(err)
		return nil, 0, errx.InternalError()
	}
	out := make([]TestView, 0, len(tests))
	for _, t := range tests {
		out = append(out, s.view(t, results[t.ID], false))
	}
	return out, total, nil
}

// GetTest is one test with every probe, the content check of its copy and,
// for a tracking comparison, the other half.
func (s *service) GetTest(ctx context.Context, orgID *uuid.UUID, id uuid.UUID) (*TestDetail, *errx.Error) {
	var test *models.PlacementTest
	var err error
	if orgID != nil {
		test, err = s.Repo.GetOrgTest(ctx, *orgID, id)
	} else {
		test, err = s.Repo.GetTest(ctx, id)
	}
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if test == nil {
		return nil, errx.New(errx.NotFound, "placement test not found")
	}
	ids := []uuid.UUID{test.ID}
	var sibling *models.PlacementTest
	if test.CompareGroupID != nil && test.OrganizationID != nil {
		group, err := s.Repo.ListGroup(ctx, *test.OrganizationID, *test.CompareGroupID)
		if err == nil {
			for i := range group {
				if group[i].ID != test.ID {
					sibling = &group[i]
					ids = append(ids, sibling.ID)
				}
			}
		}
	}
	results, err := s.Repo.ListResults(ctx, ids)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	detail := &TestDetail{
		TestView: s.view(*test, results[test.ID], true),
		Results:  resultViews(test.Panel, results[test.ID]),
		Content:  contentCheck(*test),
	}
	if sibling != nil {
		v := s.view(*sibling, results[sibling.ID], false)
		detail.Compare = &v
	}
	return detail, nil
}
