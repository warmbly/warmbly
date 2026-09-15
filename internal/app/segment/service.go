// Package segment manages saved contact audiences; membership is computed at
// read time. The one exception is campaign links: a segment attached to a
// campaign enrols its members as leads, kept current by targeted syncs on
// every membership-changing write plus a periodic sweep.
package segment

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// CampaignWaker wakes a campaign's parked send chain after leads are added,
// and restarts a finished one. Satisfied structurally by campaign.CampaignService.
type CampaignWaker interface {
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)
}

type Service interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.Segment, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.Segment, *errx.Error)
	Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	Preview(ctx context.Context, orgID uuid.UUID, in *models.SegmentPreview) (int, *errx.Error)
	SetMembers(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentMembersWrite) (int, *errx.Error)
	MemberModes(ctx context.Context, orgID, id uuid.UUID, contactIDs []string) (map[uuid.UUID]models.SegmentMemberMode, *errx.Error)
	AddToCampaign(ctx context.Context, orgID uuid.UUID, actor string, id uuid.UUID, in *models.SegmentAddToCampaign) (*models.SegmentAddToCampaignResult, *errx.Error)
	// Fields describes every filterable field for the condition builder.
	Fields(ctx context.Context, orgID uuid.UUID) ([]models.SegmentFieldSpec, *errx.Error)
	SegmentsForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactSegment, *errx.Error)
	Overrides(ctx context.Context, orgID, id uuid.UUID) ([]models.SegmentOverride, *errx.Error)
	// ListCampaignSegments lists the segments linked to a campaign.
	ListCampaignSegments(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.CampaignSegmentLink, *errx.Error)
	// SetCampaignSegments replaces a campaign's linked segments, enrols their
	// current members immediately and withdraws the audience a detached link
	// brought. Returns the links, how many leads were new, and what the
	// detachment took back.
	SetCampaignSegments(ctx context.Context, orgID, campaignID uuid.UUID, in *models.CampaignSegmentsWrite) ([]models.CampaignSegmentLink, int, models.CampaignAudienceChange, *errx.Error)
	// SyncOrgLinkedCampaigns re-enrols every linked campaign of the org after
	// contacts changed. Best effort: the sweep is the backstop.
	SyncOrgLinkedCampaigns(ctx context.Context, orgID uuid.UUID)
	// StartCampaignSegmentSync sweeps every linked campaign on an interval so
	// membership drift (dates, engagement, nested segments) still enrols.
	StartCampaignSegmentSync(ctx context.Context, interval time.Duration)
	SetCampaignWaker(w CampaignWaker)
	SetEnrolmentAuditor(a EnrolmentAuditor)
}

// EnrolmentAuditor records an automatic enrolment as a campaign update, so the
// audit spine refreshes every teammate's Leads tab when the sweep adds leads.
type EnrolmentAuditor interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// CustomFieldLister is the slice of the contact repository Fields needs.
type CustomFieldLister interface {
	DistinctCustomFieldKeys(ctx context.Context, orgID uuid.UUID) ([]string, error)
}

type service struct {
	repo   repository.SegmentRepository
	fields CustomFieldLister
	waker  CampaignWaker
	audit  EnrolmentAuditor
	// orgSync coalesces org-wide enrolment passes, one entry per org that is
	// currently syncing. Guarded by syncMu, which owns every transition so an
	// entry is only dropped when nothing is running or queued.
	syncMu  sync.Mutex
	orgSync map[uuid.UUID]*orgSyncState
}

// orgSyncState is a running pass plus a "do it once more" flag. A write that
// lands mid-pass cannot be served by that pass (it read the old membership),
// so it asks for a follow-up instead of being dropped.
type orgSyncState struct {
	running bool
	again   bool
}

func NewService(repo repository.SegmentRepository, fields CustomFieldLister) Service {
	return &service{repo: repo, fields: fields, orgSync: map[uuid.UUID]*orgSyncState{}}
}

func (s *service) SetCampaignWaker(w CampaignWaker)       { s.waker = w }
func (s *service) SetEnrolmentAuditor(a EnrolmentAuditor) { s.audit = a }

func (s *service) List(ctx context.Context, orgID uuid.UUID) ([]models.Segment, *errx.Error) {
	return s.repo.List(ctx, orgID)
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Segment, *errx.Error) {
	return s.repo.Get(ctx, orgID, id)
}

// applyWrite folds a create/update body into seg, validating each field it
// sets. selfID guards self-reference on update.
func applyWrite(seg *models.Segment, in *models.SegmentWrite, selfID *uuid.UUID) *errx.Error {
	if in.Name != nil {
		name, xerr := models.ValidateSegmentName(*in.Name)
		if xerr != nil {
			return xerr
		}
		seg.Name = name
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		if len(d) > models.SegmentMaxDescLen {
			return errx.New(errx.BadRequest, fmt.Sprintf("description must be at most %d characters", models.SegmentMaxDescLen))
		}
		seg.Description = d
	}
	if in.Color != nil {
		color, xerr := models.ValidateSegmentColor(*in.Color)
		if xerr != nil {
			return xerr
		}
		seg.Color = color
	}
	if in.Match != nil {
		if xerr := models.ValidateSegmentMatch(*in.Match); xerr != nil {
			return xerr
		}
		seg.Match = *in.Match
	}
	if in.Conditions != nil {
		conds := *in.Conditions
		if conds == nil {
			conds = []models.SegmentCondition{}
		}
		if xerr := models.ValidateSegmentConditions(conds, selfID); xerr != nil {
			return xerr
		}
		seg.Conditions = conds
	}
	return nil
}

// checkReferences makes sure every referenced segment exists in the org and
// that following references from it never leads back to selfID.
func (s *service) checkReferences(ctx context.Context, orgID uuid.UUID, conds []models.SegmentCondition, selfID *uuid.UUID) *errx.Error {
	refs := models.SegmentReferences(conds)
	if len(refs) == 0 {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	pending := refs
	for depth := 0; len(pending) > 0; depth++ {
		if depth > models.SegmentMaxNestingDeep {
			return errx.New(errx.BadRequest, fmt.Sprintf("segments can be nested at most %d levels deep", models.SegmentMaxNestingDeep))
		}
		var next []uuid.UUID
		for _, id := range pending {
			if selfID != nil && id == *selfID {
				return errx.New(errx.BadRequest, "segments cannot reference each other in a loop")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			ref, xerr := s.repo.Get(ctx, orgID, id)
			if xerr != nil {
				if xerr.Code == errx.NotFound {
					return errx.New(errx.BadRequest, "a referenced segment does not exist")
				}
				return xerr
			}
			next = append(next, models.SegmentReferences(ref.Conditions)...)
		}
		pending = next
	}
	return nil
}

func (s *service) Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error) {
	seg := &models.Segment{Color: "#0284c7", Match: models.SegmentMatchAll, Conditions: []models.SegmentCondition{}}
	if in.Name == nil {
		return nil, errx.New(errx.BadRequest, "segment name is required")
	}
	if xerr := applyWrite(seg, in, nil); xerr != nil {
		return nil, xerr
	}
	if xerr := s.checkReferences(ctx, orgID, seg.Conditions, nil); xerr != nil {
		return nil, xerr
	}
	return s.repo.Create(ctx, orgID, createdBy, seg)
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error) {
	seg, xerr := s.repo.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := applyWrite(seg, in, &id); xerr != nil {
		return nil, xerr
	}
	if in.Conditions != nil {
		if xerr := s.checkReferences(ctx, orgID, seg.Conditions, &id); xerr != nil {
			return nil, xerr
		}
	}
	out, xerr := s.repo.Update(ctx, orgID, seg)
	if xerr != nil {
		return nil, xerr
	}
	// A definition change can admit new members; enrol them into linked
	// campaigns right away instead of waiting for the sweep.
	if in.Conditions != nil || in.Match != nil {
		s.syncLinkedCampaignsForSegments(ctx, orgID, []uuid.UUID{id})
	}
	return out, nil
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	names, xerr := s.repo.ReferencedBy(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	if len(names) > 0 {
		return errx.New(errx.Conflict, "this segment is used by: "+strings.Join(names, ", ")+". Remove it from those segments first")
	}
	campaigns, xerr := s.repo.CampaignsUsingSegment(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	if len(campaigns) > 0 {
		return errx.New(errx.Conflict, "this segment is linked to: "+strings.Join(campaigns, ", ")+". Detach it from those campaigns first")
	}
	return s.repo.Delete(ctx, orgID, id)
}

func (s *service) Preview(ctx context.Context, orgID uuid.UUID, in *models.SegmentPreview) (int, *errx.Error) {
	if in.Match == "" {
		in.Match = models.SegmentMatchAll
	}
	if xerr := models.ValidateSegmentMatch(in.Match); xerr != nil {
		return 0, xerr
	}
	if in.Conditions == nil {
		in.Conditions = []models.SegmentCondition{}
	}
	if xerr := models.ValidateSegmentConditions(in.Conditions, in.ID); xerr != nil {
		return 0, xerr
	}
	if xerr := s.checkReferences(ctx, orgID, in.Conditions, in.ID); xerr != nil {
		return 0, xerr
	}
	if in.ID != nil {
		if _, xerr := s.repo.Get(ctx, orgID, *in.ID); xerr != nil {
			return 0, xerr
		}
	}
	return s.repo.Count(ctx, orgID, in.ID, in.Match, in.Conditions)
}

func parseContactIDs(raw []string) ([]uuid.UUID, *errx.Error) {
	if len(raw) == 0 {
		return nil, errx.New(errx.BadRequest, "no contacts provided")
	}
	if len(raw) > 1000 {
		return nil, errx.New(errx.BadRequest, "at most 1000 contacts per request")
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, r := range raw {
		id, err := uuid.Parse(r)
		if err != nil {
			return nil, errx.New(errx.BadRequest, "invalid contact id")
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *service) SetMembers(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentMembersWrite) (int, *errx.Error) {
	switch in.Mode {
	case models.SegmentMemberInclude, models.SegmentMemberExclude, models.SegmentMemberAuto:
	default:
		return 0, errx.New(errx.BadRequest, "mode must be include, exclude or auto")
	}
	ids, xerr := parseContactIDs(in.Contacts)
	if xerr != nil {
		return 0, xerr
	}
	if _, xerr := s.repo.Get(ctx, orgID, id); xerr != nil {
		return 0, xerr
	}
	n, xerr := s.repo.SetMembers(ctx, orgID, id, ids, in.Mode)
	if xerr != nil {
		return 0, xerr
	}
	// A pin-in (or a cleared exclude) can admit new members.
	if in.Mode != models.SegmentMemberExclude {
		s.syncLinkedCampaignsForSegments(ctx, orgID, []uuid.UUID{id})
	}
	return n, nil
}

func (s *service) MemberModes(ctx context.Context, orgID, id uuid.UUID, contactIDs []string) (map[uuid.UUID]models.SegmentMemberMode, *errx.Error) {
	ids, xerr := parseContactIDs(contactIDs)
	if xerr != nil {
		return nil, xerr
	}
	if _, xerr := s.repo.Get(ctx, orgID, id); xerr != nil {
		return nil, xerr
	}
	return s.repo.MemberModes(ctx, id, ids)
}

func (s *service) AddToCampaign(ctx context.Context, orgID uuid.UUID, actor string, id uuid.UUID, in *models.SegmentAddToCampaign) (*models.SegmentAddToCampaignResult, *errx.Error) {
	campaignID, err := uuid.Parse(in.CampaignID)
	if err != nil {
		return nil, errx.New(errx.BadRequest, "invalid campaign id")
	}
	res, xerr := s.repo.AddToCampaign(ctx, orgID, actor, id, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	s.reactToEnrolment(ctx, models.LinkedCampaign{CampaignID: campaignID, OrganizationID: orgID, Status: res.Status}, res.Added)
	return res, nil
}

func (s *service) ListCampaignSegments(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.CampaignSegmentLink, *errx.Error) {
	return s.repo.ListForCampaign(ctx, orgID, campaignID)
}

func (s *service) SetCampaignSegments(ctx context.Context, orgID, campaignID uuid.UUID, in *models.CampaignSegmentsWrite) ([]models.CampaignSegmentLink, int, models.CampaignAudienceChange, *errx.Error) {
	var none models.CampaignAudienceChange
	seen := map[uuid.UUID]bool{}
	ids := make([]uuid.UUID, 0, len(in.SegmentIDs))
	for _, raw := range in.SegmentIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, 0, none, errx.New(errx.BadRequest, "invalid segment id")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) > models.CampaignSegmentsMax {
		return nil, 0, none, errx.New(errx.BadRequest, fmt.Sprintf("a campaign can link at most %d segments", models.CampaignSegmentsMax))
	}
	// Links, withdrawal and enrolment commit together: the user is waiting on
	// this one, and a failed enrolment must not answer 200 with "added 0".
	added, change, status, xerr := s.repo.ReplaceForCampaign(ctx, orgID, campaignID, ids)
	if xerr != nil {
		return nil, 0, none, xerr
	}
	s.reactToEnrolment(ctx, models.LinkedCampaign{CampaignID: campaignID, OrganizationID: orgID, Status: status}, added)
	out, xerr := s.repo.ListForCampaign(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, 0, none, xerr
	}
	return out, added, change, nil
}

// syncLinkedCampaign enrols missing leads for one linked campaign, waking an
// active chain and restarting a completed one when anything was added.
func (s *service) syncLinkedCampaign(ctx context.Context, lc models.LinkedCampaign) (int, *errx.Error) {
	added, xerr := s.repo.SyncCampaignSegments(ctx, lc.OrganizationID, lc.CampaignID)
	if xerr != nil {
		log.Warn().Str("campaign_id", lc.CampaignID.String()).Str("error", xerr.Message).Msg("segment sync: enrol failed")
		return 0, xerr
	}
	s.reactToEnrolment(ctx, lc, added)
	if added > 0 && s.audit != nil {
		// The request paths audit themselves; this is the platform acting
		// (zero actor), and it is what tells open Leads tabs to refresh.
		s.audit.LogAction(ctx, lc.OrganizationID, uuid.Nil, models.AuditActionUpdate, models.AuditEntityCampaign, &lc.CampaignID, "", "",
			map[string]string{"leads_added": strconv.Itoa(added)}, map[string]string{"source": "segment_sync"})
	}
	return added, nil
}

// reactToEnrolment wakes an active campaign and restarts a finished one when
// new leads arrived. WakeCampaigns owns both (the finished case goes through
// the full launch checks), so a segment enrolment, a direct add and an
// automation all behave the same way. Paused and draft campaigns accumulate.
func (s *service) reactToEnrolment(ctx context.Context, lc models.LinkedCampaign, added int) {
	if added == 0 || s.waker == nil {
		return
	}
	switch lc.Status {
	case "active", "completed":
		s.waker.WakeCampaigns(ctx, lc.OrganizationID, []string{lc.CampaignID.String()})
	}
}

// syncLinkedCampaignsForSegments re-enrols the campaigns linked to any of the
// given segments. Nested references (a linked segment built on this one) are
// not chased here; the periodic sweep covers them. Detached from the caller's
// request: the enrolment scans grow with the contact list, and this is
// declared best effort with the sweep as the backstop.
func (s *service) syncLinkedCampaignsForSegments(ctx context.Context, orgID uuid.UUID, segmentIDs []uuid.UUID) {
	bg := context.WithoutCancel(ctx)
	go func() {
		rctx, cancel := context.WithTimeout(bg, 2*time.Minute)
		defer cancel()
		links, xerr := s.repo.LinkedCampaignsForSegments(rctx, orgID, segmentIDs)
		if xerr != nil {
			return
		}
		for _, lc := range links {
			_, _ = s.syncLinkedCampaign(rctx, lc)
		}
	}()
}

func (s *service) SyncOrgLinkedCampaigns(ctx context.Context, orgID uuid.UUID) {
	// One pass per org at a time, so a burst of contact writes cannot stack
	// org-wide enrolment scans. Requests that arrive mid-pass are coalesced
	// into a single follow-up rather than dropped: an import writes its
	// segment membership after the contact rows, so the pass already running
	// read the old membership and would leave those contacts to the sweep.
	s.syncMu.Lock()
	st := s.orgSync[orgID]
	if st == nil {
		st = &orgSyncState{}
		s.orgSync[orgID] = st
	}
	if st.running {
		st.again = true
		s.syncMu.Unlock()
		return
	}
	st.running = true
	s.syncMu.Unlock()

	bg := context.WithoutCancel(ctx)
	go func() {
		for {
			s.runOrgSyncPass(bg, orgID)
			s.syncMu.Lock()
			if st.again {
				st.again = false
				s.syncMu.Unlock()
				continue
			}
			delete(s.orgSync, orgID)
			s.syncMu.Unlock()
			return
		}
	}()
}

// runOrgSyncPass enrols every linked campaign in one organization once.
func (s *service) runOrgSyncPass(ctx context.Context, orgID uuid.UUID) {
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	links, xerr := s.repo.LinkedCampaigns(rctx, &orgID)
	if xerr != nil {
		return
	}
	for _, lc := range links {
		_, _ = s.syncLinkedCampaign(rctx, lc)
	}
}

func (s *service) StartCampaignSegmentSync(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Sweep once on boot so links catch up promptly after a restart.
	s.sweepLinkedCampaigns(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepLinkedCampaigns(ctx)
		}
	}
}

func (s *service) sweepLinkedCampaigns(ctx context.Context) {
	scanCtx, scanCancel := context.WithTimeout(ctx, 30*time.Second)
	links, xerr := s.repo.LinkedCampaigns(scanCtx, nil)
	scanCancel()
	if xerr != nil {
		log.Warn().Str("error", xerr.Message).Msg("segment sync: sweep scan failed")
		return
	}
	total := 0
	// Each campaign gets its own deadline: one shared budget would starve the
	// same tail campaigns every pass once the instance holds enough links.
	for _, lc := range links {
		if ctx.Err() != nil {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		n, _ := s.syncLinkedCampaign(cctx, lc)
		total += n
		cancel()
	}
	if total > 0 {
		log.Info().Int("added", total).Msg("segment sync: sweep enrolled new leads")
	}
}

func (s *service) Fields(ctx context.Context, orgID uuid.UUID) ([]models.SegmentFieldSpec, *errx.Error) {
	out := make([]models.SegmentFieldSpec, 0, len(models.SegmentFieldCatalog)+16)
	out = append(out, models.SegmentFieldCatalog...)
	if s.fields == nil {
		return out, nil
	}
	keys, err := s.fields.DistinctCustomFieldKeys(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	for _, k := range keys {
		out = append(out, models.SegmentFieldSpec{Field: models.SegmentCustomFieldPrefix + k, Label: k, Group: "Custom field", Kind: models.SegmentFieldText})
	}
	return out, nil
}

func (s *service) SegmentsForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactSegment, *errx.Error) {
	return s.repo.SegmentsForContact(ctx, orgID, contactID)
}

func (s *service) Overrides(ctx context.Context, orgID, id uuid.UUID) ([]models.SegmentOverride, *errx.Error) {
	if _, xerr := s.repo.Get(ctx, orgID, id); xerr != nil {
		return nil, xerr
	}
	return s.repo.ListOverrides(ctx, orgID, id)
}
