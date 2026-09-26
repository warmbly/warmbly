package placement

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
	"github.com/warmbly/warmbly/internal/repository"
)

// TestView is a test with where its copies landed, overall and per host
// family. The copy itself is left out of lists.
type TestView struct {
	models.PlacementTest
	Summary  models.PlacementCounts         `json:"summary"`
	Families []models.PlacementFamilyCounts `json:"families"`
}

// ResultView is one probe as the dashboard shows it.
type ResultView struct {
	// Seed is the seed's address, masked on the shared panels so the panel
	// cannot be listed and whitelisted.
	Seed        string     `json:"seed"`
	Family      string     `json:"family"`
	FamilyLabel string     `json:"family_label"`
	Folder      string     `json:"folder"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	SentAt      *time.Time `json:"sent_at"`
	DetectedAt  *time.Time `json:"detected_at"`
	Error       string     `json:"error,omitempty"`
}

// ContentCheck is the rules pass over the test's copy.
type ContentCheck struct {
	Score  int              `json:"score"`
	Issues []warmlint.Issue `json:"issues"`
}

// TestDetail is one test in full.
type TestDetail struct {
	TestView
	Results []ResultView `json:"results"`
	Content ContentCheck `json:"content"`
	Compare *TestView    `json:"compare,omitempty"`
}

// Overview is what a workspace can test on and how much of its allowance is
// left.
type Overview struct {
	Panels         []models.PlacementPanelInfo `json:"panels"`
	Usage          models.PlacementUsage       `json:"usage"`
	WorkspaceSeeds int                         `json:"workspace_seeds"`
	SeedsPerTest   int                         `json:"seeds_per_test"`
	SpacingSeconds int                         `json:"spacing_seconds"`
}

// AdminSeed is a mailbox on (or offered for) the instance panel.
type AdminSeed struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID *uuid.UUID `json:"organization_id"`
	Email          string     `json:"email"`
	Name           string     `json:"name"`
	Provider       string     `json:"provider"`
	Family         string     `json:"family"`
	FamilyLabel    string     `json:"family_label"`
	Status         string     `json:"status"`
	WorkerID       *uuid.UUID `json:"worker_id"`
	SeedScope      string     `json:"seed_scope"`
}

func familyLabel(f string) string {
	if l := mailhost.Host(f).Label(); l != "" {
		return l
	}
	return "Other provider"
}

// view counts a test's probes overall and per family. withBody keeps the copy.
func (s *service) view(t models.PlacementTest, results []models.PlacementResult, withBody bool) TestView {
	if !withBody {
		t.BodyHTML, t.BodyPlain = "", ""
	}
	v := TestView{PlacementTest: t, Families: []models.PlacementFamilyCounts{}}
	byFamily := map[string]*models.PlacementCounts{}
	for _, r := range results {
		v.Summary.Add(r.Folder)
		c, ok := byFamily[r.Family]
		if !ok {
			c = &models.PlacementCounts{}
			byFamily[r.Family] = c
		}
		c.Add(r.Folder)
	}
	v.Summary.Finish()
	for fam, c := range byFamily {
		c.Finish()
		v.Families = append(v.Families, models.PlacementFamilyCounts{Family: fam, Label: familyLabel(fam), Counts: *c})
	}
	sort.Slice(v.Families, func(i, j int) bool { return v.Families[i].Label < v.Families[j].Label })
	return v
}

func resultViews(panel string, results []models.PlacementResult) []ResultView {
	out := make([]ResultView, 0, len(results))
	for _, r := range results {
		seed := r.SeedAddress
		if panel != models.PlacementPanelWorkspace {
			seed = maskAddress(seed)
		}
		out = append(out, ResultView{
			Seed:        seed,
			Family:      r.Family,
			FamilyLabel: familyLabel(r.Family),
			Folder:      r.Folder,
			ScheduledAt: r.ScheduledAt,
			SentAt:      r.SentAt,
			DetectedAt:  r.DetectedAt,
			Error:       r.Error,
		})
	}
	return out
}

// maskAddress keeps the first letter of the local part and the domain.
func maskAddress(address string) string {
	at := strings.LastIndex(address, "@")
	if at <= 0 {
		return "***"
	}
	return address[:1] + "***" + address[at:]
}

func contentCheck(t models.PlacementTest) ContentCheck {
	res := warmlint.Score(t.Subject, t.BodyHTML, t.BodyPlain)
	return ContentCheck{Score: res.Score, Issues: res.Issues}
}

// usage is a workspace's count against the monthly allowance on the metered
// panel. A self-hosted instance does not meter its own seeds.
func (s *service) usage(ctx context.Context, orgID uuid.UUID) (models.PlacementUsage, *errx.Error) {
	now := s.now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	u := models.PlacementUsage{PeriodStart: start, PeriodEnd: start.AddDate(0, 1, 0)}
	used, err := s.Repo.CountMeteredTests(ctx, orgID, start)
	if err != nil {
		errs.CaptureException(err)
		return u, errx.InternalError()
	}
	u.Used = used
	if config.SelfHosted() {
		return u, nil
	}
	pol := s.policy(ctx)
	limit := pol.TestsPerMonthTrial
	if s.Gate != nil {
		if paid, _ := s.Gate.IsPaidOrganization(ctx, orgID); paid {
			limit = pol.TestsPerMonthPaid
		}
	}
	u.Limit = &limit
	return u, nil
}

func panelInfo(panel string, seeds []models.PlacementSeed, metered bool) models.PlacementPanelInfo {
	info := models.PlacementPanelInfo{Panel: panel, Seeds: len(seeds), Metered: metered, Families: []models.PlacementPanelFamily{}}
	counts := map[string]int{}
	for _, s := range seeds {
		counts[s.Family]++
	}
	for fam, n := range counts {
		info.Families = append(info.Families, models.PlacementPanelFamily{Family: fam, Label: familyLabel(fam), Seeds: n})
	}
	sort.Slice(info.Families, func(i, j int) bool { return info.Families[i].Label < info.Families[j].Label })
	info.Available = len(seeds) > 0
	return info
}

// Overview lists the three panels with their seed mix, and the allowance.
func (s *service) Overview(ctx context.Context, orgID uuid.UUID) (*Overview, *errx.Error) {
	pol := s.policy(ctx)
	out := &Overview{SeedsPerTest: pol.SeedsPerTest, SpacingSeconds: pol.SpacingSeconds}
	usage, xerr := s.usage(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	out.Usage = usage

	instance, err := s.Repo.ListSeeds(ctx, models.SeedScopeInstance, nil, true)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	inst := panelInfo(models.PlacementPanelInstance, familySeeds(instance), !config.SelfHosted())
	if !inst.Available {
		inst.Reason = "No seed inboxes are on this instance's panel yet."
		if config.SelfHosted() {
			inst.Reason = "No seed inboxes are on this instance's panel yet. An administrator adds them in the admin panel."
		}
	}
	out.Panels = append(out.Panels, inst)

	own, err := s.Repo.ListSeeds(ctx, models.SeedScopeWorkspace, &orgID, true)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	ws := panelInfo(models.PlacementPanelWorkspace, familySeeds(own), false)
	if !ws.Available {
		ws.Reason = "Mark one of this workspace's mailboxes as a seed inbox to test on it."
	}
	out.Panels = append(out.Panels, ws)
	out.WorkspaceSeeds = len(own)

	cloud := models.PlacementPanelInfo{Panel: models.PlacementPanelCloud, Metered: true, Families: []models.PlacementPanelFamily{}}
	switch {
	case !config.SelfHosted():
		// The hosted product's own panel is the instance panel.
		cloud.Reason = "Warmbly Cloud's panel is the instance panel here."
	case s.Cloud == nil:
		cloud.Reason = "Link this instance to Warmbly Cloud to test on its seed panel."
	default:
		if panel, xerr := s.Cloud.PlacementPanel(ctx); xerr != nil {
			cloud.Reason = xerr.Message
		} else if panel != nil {
			cloud = panel.Panel
			cloud.Panel = models.PlacementPanelCloud
			cloud.Metered = true
		}
	}
	if config.SelfHosted() || cloud.Available {
		out.Panels = append(out.Panels, cloud)
	}
	return out, nil
}

func familySeeds(rows []repository.SeedAccount) []models.PlacementSeed {
	out := make([]models.PlacementSeed, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.PlacementSeed{Address: r.Email, Family: string(mailhost.ForMailbox(r.MailHost, r.Provider, r.Email))})
	}
	return out
}
