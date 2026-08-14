package confenge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// memRepo is an in-memory OutreachRepository for unit tests of ImportFromBytes.
type memRepo struct {
	mu                 sync.Mutex
	runs               map[uuid.UUID]*models.OutreachImportRun
	byIdem             map[string]*models.OutreachImportRun
	accounts           map[string]*models.OutreachAccount
	byID               map[uuid.UUID]*models.OutreachAccount
	cands              map[uuid.UUID][]models.OutreachContactCandidate
	evidence           map[uuid.UUID][]models.OutreachEvidence
	drafts             map[uuid.UUID]*models.OutreachDraft
	outcomes           []models.OutreachOutcome
	touchpoints        map[uuid.UUID]*models.OutreachTouchpoint
	outcomeBy          map[string]*models.OutreachOutcome
	orgOwner           map[uuid.UUID]uuid.UUID
	feedSync           map[uuid.UUID]*models.OutreachFeedSyncState
	advLocks           map[int64]bool
	settings           map[uuid.UUID]*models.OutreachOrgSettings
	pilotMemberships   map[uuid.UUID]*models.OutreachPilotMembership
	pilotOperations    map[string]string
	pilotSlots         map[string]string
	listTouchpointsErr error
	upsertDraftErr     error
	actions            map[uuid.UUID]*models.OutreachCommercialAction
	actionByIdem       map[string]*models.OutreachCommercialAction
}

func newMemRepo() *memRepo {
	return &memRepo{
		runs: map[uuid.UUID]*models.OutreachImportRun{}, byIdem: map[string]*models.OutreachImportRun{},
		accounts: map[string]*models.OutreachAccount{}, byID: map[uuid.UUID]*models.OutreachAccount{},
		cands: map[uuid.UUID][]models.OutreachContactCandidate{}, evidence: map[uuid.UUID][]models.OutreachEvidence{},
		drafts: map[uuid.UUID]*models.OutreachDraft{}, touchpoints: map[uuid.UUID]*models.OutreachTouchpoint{},
		outcomeBy: map[string]*models.OutreachOutcome{}, orgOwner: map[uuid.UUID]uuid.UUID{},
		settings:         map[uuid.UUID]*models.OutreachOrgSettings{},
		pilotMemberships: map[uuid.UUID]*models.OutreachPilotMembership{},
		pilotOperations:  map[string]string{},
		pilotSlots:       map[string]string{},
		actions:          map[uuid.UUID]*models.OutreachCommercialAction{},
		actionByIdem:     map[string]*models.OutreachCommercialAction{},
	}
}

func accKey(org uuid.UUID, cnpj string) string { return org.String() + "|" + cnpj }

func (m *memRepo) CreateImportRun(ctx context.Context, run *models.OutreachImportRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	cp := *run
	m.runs[run.ID] = &cp
	if run.IdempotencyKey != "" {
		m.byIdem[run.OrganizationID.String()+"|"+run.IdempotencyKey] = &cp
	}
	return nil
}

func (m *memRepo) UpdateImportRun(ctx context.Context, run *models.OutreachImportRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *run
	m.runs[run.ID] = &cp
	if run.IdempotencyKey != "" {
		m.byIdem[run.OrganizationID.String()+"|"+run.IdempotencyKey] = &cp
	}
	return nil
}

func (m *memRepo) GetImportRun(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachImportRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id]
	if r == nil || r.OrganizationID != orgID {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *memRepo) GetImportRunByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachImportRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byIdem[orgID.String()+"|"+key]
	if r == nil {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *memRepo) ListImportRuns(ctx context.Context, orgID uuid.UUID, limit int) ([]models.OutreachImportRun, error) {
	return nil, nil
}

func (m *memRepo) GetAccountByCNPJ(ctx context.Context, orgID uuid.UUID, cnpj14 string) (*models.OutreachAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.accounts[accKey(orgID, cnpj14)]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memRepo) GetAccount(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.byID[id]
	if a == nil || a.OrganizationID != orgID {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memRepo) UpsertAccount(ctx context.Context, acc *models.OutreachAccount) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := accKey(acc.OrganizationID, acc.CNPJ14)
	if acc.SourceSystem == "" && acc.TargetFitClass == "" {
		markTestAccountTargetFitReady(acc)
	}
	existing := m.accounts[k]
	created := existing == nil
	if existing != nil {
		acc.ID = existing.ID
		// preserve DNC
		if existing.DoNotContact {
			acc.DoNotContact = true
		}
	} else if acc.ID == uuid.Nil {
		acc.ID = uuid.New()
	}
	cp := *acc
	m.accounts[k] = &cp
	m.byID[cp.ID] = &cp
	return created, nil
}

func markTestAccountTargetFitReady(acc *models.OutreachAccount) {
	t := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	acc.TargetFitClass = TargetFitConfirmed
	acc.TargetFitVersion = "confenge-target-fit-v1"
	acc.TargetFitSourceWatermark = t.Format(time.RFC3339)
	acc.TargetFitObservedAt = &t
	acc.TargetFitComputedAt = &t
	acc.TargetFitFresh = true
	acc.TargetFitSendTier = "A_AUTOMATIC"
	acc.TargetFitEligible = true
	acc.TargetFitSuppressionReason = ""
	acc.EmailSendReady = true
}

func (m *memRepo) ListAccounts(ctx context.Context, orgID uuid.UUID, filter repository.OutreachAccountFilter) ([]models.OutreachAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var out []models.OutreachAccount
	for _, a := range m.byID {
		if a.OrganizationID != orgID {
			continue
		}
		if filter.QueueState != "" && a.QueueState != filter.QueueState {
			continue
		}
		if filter.CNPJ14 != "" && a.CNPJ14 != filter.CNPJ14 {
			continue
		}
		if filter.ActivationState != "" && a.ActivationState != filter.ActivationState {
			continue
		}
		if filter.ActivationDueNow {
			if a.NextBestActionAt != nil && a.NextBestActionAt.After(now) {
				continue
			}
		}
		if filter.ActivationNotExpired {
			if a.ActivationExpiresAt != nil && !a.ActivationExpiresAt.After(now) {
				continue
			}
		}
		if filter.ExcludeTerminal {
			switch a.QueueState {
			case models.OutreachQueueDoNotContact, models.OutreachQueueBlocked, models.OutreachQueueBounced,
				models.OutreachQueueReplied, models.OutreachQueueMeeting, models.OutreachQueueProposal,
				models.OutreachQueueWon, models.OutreachQueueLost, models.OutreachQueueSent, models.OutreachQueueEnrolled:
				continue
			}
			if a.DoNotContact || a.Blocked {
				continue
			}
		}
		if filter.RequireTargetFitEligible || filter.RequireOperational {
			if EvaluateTargetFit(a).Eligible == false || !a.TargetFitEligible {
				continue
			}
		}
		if filter.RequireOperational {
			if !a.TargetFitEligible || !a.EmailSendReady {
				continue
			}
			ready := false
			candidates := m.cands[a.ID]
			for i := range candidates {
				c := &candidates[i]
				if RequireEmailOutbound(a, c) == nil {
					ready = true
					break
				}
			}
			if !ready {
				continue
			}
		}
		if filter.Search != "" {
			q := strings.ToLower(filter.Search)
			blob := strings.ToLower(a.RazaoSocial + " " + a.NomeFantasia + " " + a.CNPJ14)
			if !strings.Contains(blob, q) {
				continue
			}
		}
		cp := *a
		out = append(out, cp)
	}
	// stable-ish order by CNPJ
	limit := filter.Limit
	if limit <= 0 {
		limit = len(out)
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (m *memRepo) CountByQueueState(ctx context.Context, orgID uuid.UUID) (*models.OutreachQueueSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &models.OutreachQueueSummary{}
	for _, a := range m.byID {
		if a.OrganizationID != orgID {
			continue
		}
		s.Total++
		switch a.QueueState {
		case models.OutreachQueueNeedsContact:
			s.NeedsContact++
		case models.OutreachQueueReadyToGenerate:
			s.ReadyToGenerate++
		case models.OutreachQueueNeedsReview:
			s.NeedsReview++
		case models.OutreachQueueApproved:
			s.Approved++
		case models.OutreachQueueEnrolled:
			s.Enrolled++
		case models.OutreachQueueSent:
			s.Sent++
		case models.OutreachQueueReplied:
			s.Replied++
		case models.OutreachQueueMeeting:
			s.Meeting++
		case models.OutreachQueueProposal:
			s.Proposal++
		case models.OutreachQueueWon:
			s.Won++
		case models.OutreachQueueBlocked:
			s.Blocked++
		case models.OutreachQueueBounced:
			s.Bounced++
		case models.OutreachQueueDoNotContact:
			s.DoNotContact++
		}
	}
	return s, nil
}

func (m *memRepo) SetAccountHumanFlags(ctx context.Context, orgID, id uuid.UUID, blocked, dnc bool, reason, queueState string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.byID[id]
	if a == nil || a.OrganizationID != orgID {
		return context.Canceled
	}
	a.Blocked = blocked
	a.DoNotContact = dnc
	a.BlockReason = reason
	a.QueueState = queueState
	a.HumanOverride = true
	return nil
}

func (m *memRepo) InvalidateAccountOutboundForTargetFit(ctx context.Context, orgID, accountID uuid.UUID, reason string) (repository.TargetFitInvalidationCounts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out repository.TargetFitInvalidationCounts
	for _, tp := range m.touchpoints {
		if tp.OrganizationID == orgID && tp.AccountID == accountID && models.TouchpointOpenStates[tp.State] {
			tp.State, tp.StopReason = models.TouchpointCancelled, reason
			ClearApproval(tp)
			out.Touchpoints++
		}
	}
	for _, d := range m.drafts {
		if d.OrganizationID != orgID || d.AccountID != accountID {
			continue
		}
		switch d.Status {
		case models.OutreachDraftNotGenerated, models.OutreachDraftGenerating, models.OutreachDraftNeedsReview,
			models.OutreachDraftApproved, models.OutreachDraftEnrolled:
			d.Status = models.OutreachDraftBlocked
			out.Drafts++
		}
	}
	return out, nil
}

func (m *memRepo) InvalidateAccountApprovalsForContext(ctx context.Context, orgID, accountID uuid.UUID, currentContextHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, touchpoint := range m.touchpoints {
		if touchpoint.OrganizationID != orgID || touchpoint.AccountID != accountID || touchpoint.GeneratedContextHash == currentContextHash {
			continue
		}
		if touchpoint.State == models.TouchpointApproved || touchpoint.State == models.TouchpointQueued {
			ClearApproval(touchpoint)
			touchpoint.State = models.TouchpointNeedsReview
			touchpoint.StopReason = "context_stale"
			if touchpoint.DraftID != nil {
				if draft := m.drafts[*touchpoint.DraftID]; draft != nil {
					draft.Status = models.OutreachDraftNeedsReview
					draft.ApprovedBy, draft.ApprovedAt = nil, nil
					draft.CampaignID, draft.EnrollmentContactID, draft.EnrolledAt = nil, nil, nil
				}
			}
		}
	}
	return nil
}

func (m *memRepo) ListCandidates(ctx context.Context, orgID, accountID uuid.UUID) ([]models.OutreachContactCandidate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]models.OutreachContactCandidate{}, m.cands[accountID]...), nil
}

func (m *memRepo) UpsertCandidate(ctx context.Context, c *models.OutreachContactCandidate) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.LastImportRunID == nil && c.Email != "" && c.VerificationStatus != models.OutreachVerifyCandidateUnverified &&
		!models.OutreachUnenrollableVerification[c.VerificationStatus] {
		c.EmailSendReady = true
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	list := m.cands[c.AccountID]
	for i, existing := range list {
		if existing.ID == c.ID || (existing.SourceContactID != "" && existing.SourceContactID == c.SourceContactID) {
			if existing.DoNotContact {
				c.DoNotContact = true
			}
			c.ID = existing.ID
			list[i] = *c
			m.cands[c.AccountID] = list
			return false, nil
		}
	}
	m.cands[c.AccountID] = append(list, *c)
	return true, nil
}

func (m *memRepo) GetCandidate(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachContactCandidate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, list := range m.cands {
		for i := range list {
			if list[i].ID == id && list[i].OrganizationID == orgID {
				cp := list[i]
				return &cp, nil
			}
		}
	}
	return nil, nil
}

func (m *memRepo) ListEvidence(ctx context.Context, orgID, accountID uuid.UUID) ([]models.OutreachEvidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]models.OutreachEvidence{}, m.evidence[accountID]...), nil
}

func (m *memRepo) UpsertEvidence(ctx context.Context, e *models.OutreachEvidence) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	list := m.evidence[e.AccountID]
	for i, existing := range list {
		if existing.SourceEvidenceID == e.SourceEvidenceID {
			e.ID = existing.ID
			list[i] = *e
			m.evidence[e.AccountID] = list
			return false, nil
		}
	}
	m.evidence[e.AccountID] = append(list, *e)
	return true, nil
}

func (m *memRepo) UpsertDraft(ctx context.Context, d *models.OutreachDraft) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upsertDraftErr != nil {
		return m.upsertDraftErr
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	cp := *d
	m.drafts[d.ID] = &cp
	return nil
}
func (m *memRepo) GetDraft(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachDraft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.drafts[id]
	if d == nil || d.OrganizationID != orgID {
		return nil, nil
	}
	cp := *d
	return &cp, nil
}
func (m *memRepo) GetActiveDraftForAccount(ctx context.Context, orgID, accountID uuid.UUID) (*models.OutreachDraft, error) {
	return nil, nil
}
func (m *memRepo) ListDrafts(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]models.OutreachDraft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.OutreachDraft
	for _, d := range m.drafts {
		if d.OrganizationID == orgID && (status == "" || d.Status == status) {
			out = append(out, *d)
		}
	}
	return out, nil
}
func (m *memRepo) UpdateDraftStatus(ctx context.Context, d *models.OutreachDraft) error {
	return m.UpsertDraft(ctx, d)
}
func (m *memRepo) GetOrgSettings(ctx context.Context, orgID uuid.UUID) (*models.OutreachOrgSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings[orgID]
	if s == nil {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}
func (m *memRepo) UpsertOrgSettings(ctx context.Context, s *models.OutreachOrgSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings == nil {
		m.settings = map[uuid.UUID]*models.OutreachOrgSettings{}
	}
	cp := *s
	m.settings[s.OrganizationID] = &cp
	return nil
}
func (m *memRepo) EnqueueOutcome(ctx context.Context, ev *models.OutreachOutcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.ID == uuid.Nil {
		ev.ID = uuid.New()
	}
	if ev.EventID == uuid.Nil {
		ev.EventID = uuid.New()
	}
	k := ev.OrganizationID.String() + "|" + ev.IdempotencyKey
	if _, ok := m.outcomeBy[k]; ok {
		return fmt.Errorf("duplicate outcome idempotency key")
	}
	cp := *ev
	m.outcomes = append(m.outcomes, cp)
	m.outcomeBy[k] = &cp
	return nil
}
func (m *memRepo) ListPendingOutcomes(ctx context.Context, limit int) ([]models.OutreachOutcome, error) {
	return nil, nil
}
func (m *memRepo) MarkOutcomeDelivered(ctx context.Context, orgID, id uuid.UUID) error {
	return nil
}
func (m *memRepo) MarkOutcomeAttempt(ctx context.Context, orgID, id uuid.UUID, attempts int, next time.Time, lastErr string, dead bool) error {
	return nil
}
func (m *memRepo) GetOutcomeByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.outcomeBy[orgID.String()+"|"+key]
	if o == nil {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}
func (m *memRepo) FindCandidateByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, list := range m.cands {
		for i := range list {
			c := list[i]
			if c.OrganizationID == orgID && c.Email == email {
				acc := m.byID[c.AccountID]
				return &c, acc, nil
			}
		}
	}
	return nil, nil, nil
}
func (m *memRepo) FindCandidateByEnrollment(ctx context.Context, orgID, campaignID, contactID uuid.UUID) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.drafts {
		if d.OrganizationID != orgID || d.CampaignID == nil || *d.CampaignID != campaignID || d.EnrollmentContactID == nil || *d.EnrollmentContactID != contactID || d.ContactCandidateID == nil {
			continue
		}
		for i := range m.cands[d.AccountID] {
			c := m.cands[d.AccountID][i]
			if c.ID == *d.ContactCandidateID {
				return &c, m.byID[d.AccountID], nil
			}
		}
	}
	return nil, nil, nil
}

func (m *memRepo) GetTouchpointByEnrollment(ctx context.Context, orgID, campaignID, contactID uuid.UUID) (*models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, touchpoint := range m.touchpoints {
		if touchpoint.OrganizationID != orgID || touchpoint.DraftID == nil {
			continue
		}
		draft := m.drafts[*touchpoint.DraftID]
		if draft != nil && draft.CampaignID != nil && *draft.CampaignID == campaignID && draft.EnrollmentContactID != nil && *draft.EnrollmentContactID == contactID {
			copy := *touchpoint
			return &copy, nil
		}
	}
	return nil, nil
}
func (m *memRepo) FindCandidateByPhone(ctx context.Context, orgID uuid.UUID, phone string) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, list := range m.cands {
		for i := range list {
			c := list[i]
			if c.OrganizationID != orgID {
				continue
			}
			if c.PhoneE164 == phone || c.Phone == phone {
				return &c, m.byID[c.AccountID], nil
			}
		}
	}
	return nil, nil, nil
}

func (m *memRepo) InsertTouchpoint(ctx context.Context, t *models.OutreachTouchpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	cp := *t
	m.touchpoints[t.ID] = &cp
	return nil
}
func (m *memRepo) UpdateTouchpoint(ctx context.Context, t *models.OutreachTouchpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.touchpoints[t.ID]; !ok {
		return context.Canceled
	}
	t.UpdatedAt = time.Now().UTC()
	cp := *t
	m.touchpoints[t.ID] = &cp
	return nil
}
func (m *memRepo) GetTouchpoint(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.touchpoints[id]
	if t == nil || t.OrganizationID != orgID {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}
func (m *memRepo) GetTouchpointByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.touchpoints {
		if t.OrganizationID == orgID && t.IdempotencyKey == key {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) GetTouchpointByDraft(ctx context.Context, orgID, draftID uuid.UUID) (*models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.touchpoints {
		if t.OrganizationID == orgID && t.DraftID != nil && *t.DraftID == draftID {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) ListTouchpoints(ctx context.Context, orgID, accountID uuid.UUID, state string, limit, offset int) ([]models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listTouchpointsErr != nil {
		return nil, m.listTouchpointsErr
	}
	var out []models.OutreachTouchpoint
	for _, t := range m.touchpoints {
		if t.OrganizationID != orgID || t.AccountID != accountID {
			continue
		}
		if state != "" && t.State != state {
			continue
		}
		out = append(out, *t)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Ordinal < out[i].Ordinal {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}
func (m *memRepo) ListReviewTouchpoints(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	review := map[string]bool{models.TouchpointDue: true, models.TouchpointDrafted: true, models.TouchpointNeedsReview: true, models.TouchpointApproved: true}
	var out []models.OutreachTouchpoint
	for _, t := range m.touchpoints {
		if t.OrganizationID == orgID && review[t.State] {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (m *memRepo) CASQueueTouchpoint(ctx context.Context, orgID, id uuid.UUID, expectedContentHash string) (*models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.touchpoints[id]
	if t == nil || t.OrganizationID != orgID || t.State != models.TouchpointApproved {
		return nil, nil
	}
	if t.ContentHash != expectedContentHash || t.ApprovedContentHash != t.ContentHash {
		return nil, nil
	}
	// Human path needs approved_by; CAMPAIGN_POLICY must leave approved_by nil.
	if t.AuthorizationMode == AuthorizationModeCampaignPolicy {
		if t.ApprovedBy != nil {
			return nil, nil
		}
	} else if t.ApprovedBy == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	t.State = models.TouchpointQueued
	t.QueuedAt = &now
	t.UpdatedAt = now
	cp := *t
	return &cp, nil
}
func (m *memRepo) ListDuePlannedTouchpoints(ctx context.Context, orgID uuid.UUID, now time.Time, limit int) ([]models.OutreachTouchpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var out []models.OutreachTouchpoint
	for _, t := range m.touchpoints {
		if t.OrganizationID != orgID || t.State != models.TouchpointPlanned {
			continue
		}
		if !t.DueAt.After(now) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (m *memRepo) CancelOpenTouchpoints(ctx context.Context, orgID, accountID uuid.UUID, terminalState, stopReason string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	now := time.Now().UTC()
	for _, t := range m.touchpoints {
		if t.OrganizationID != orgID || t.AccountID != accountID || !models.TouchpointOpenStates[t.State] {
			continue
		}
		t.State = terminalState
		t.StopReason = stopReason
		t.ApprovedBy = nil
		t.ApprovedAt = nil
		t.ApprovedContentHash = ""
		t.UpdatedAt = now
		n++
	}
	return n, nil
}

func (m *memRepo) GetOrgOwnerUserID(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.orgOwner[orgID]; ok {
		return id, nil
	}
	// Default: synthesize stable owner for tests so CRM tasks can be created without an explicit actor.
	id := uuid.New()
	m.orgOwner[orgID] = id
	return id, nil
}

func (m *memRepo) GetLatestOutcomeForLead(ctx context.Context, orgID uuid.UUID, cnpj14, sourceLeadID, contactEmail string) (*models.OutreachOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *models.OutreachOutcome
	for i := range m.outcomes {
		o := m.outcomes[i]
		if o.OrganizationID != orgID {
			continue
		}
		match := false
		if cnpj14 != "" && o.CNPJ14 == cnpj14 {
			match = true
		}
		if sourceLeadID != "" && o.SourceLeadID == sourceLeadID {
			match = true
		}
		if contactEmail != "" && strings.EqualFold(o.ContactEmail, contactEmail) {
			match = true
		}
		if !match {
			continue
		}
		if best == nil || o.OccurredAt.After(best.OccurredAt) {
			cp := o
			best = &cp
		}
	}
	return best, nil
}

func testSvc(repo repository.OutreachRepository) Service {
	cfg := Config{
		Enabled:              true,
		RequireHumanApproval: true,
		DefaultDailyLimit:    10,
		MaxInitialEmailWords: 120,
		MaxFeedPayloadBytes:  DefaultMaxPayloadBytes,
	}
	return NewService(cfg, repo, nil)
}

func TestImportNativeFeedCreatesAccounts(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	user := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")

	run, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{})
	if xerr != nil {
		t.Fatalf("import: %v", xerr)
	}
	if run.Status != models.OutreachImportCompleted && run.Status != models.OutreachImportPartial {
		t.Fatalf("status %s counts=%+v errs=%+v", run.Status, run.Counts, run.Errors)
	}
	if run.Counts.Creates < 4 {
		t.Fatalf("want >=4 creates, got %+v", run.Counts)
	}
	// No-email company staged as NEEDS_CONTACT
	acc, err := repo.GetAccountByCNPJ(context.Background(), org, "55444333000122")
	if err != nil || acc == nil {
		t.Fatal("missing no-contact company")
	}
	if acc.QueueState != models.OutreachQueueNeedsContact {
		t.Fatalf("queue_state=%s want NEEDS_CONTACT", acc.QueueState)
	}
	// Official contact company ready
	acme, _ := repo.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if acme == nil || acme.QueueState != models.OutreachQueueReadyToGenerate {
		t.Fatalf("acme state=%v", acme)
	}
	// Unverified-only contact is not enrollable => NEEDS_CONTACT
	cn, _ := repo.GetAccountByCNPJ(context.Background(), org, "66777888000199")
	if cn == nil || cn.QueueState != models.OutreachQueueNeedsContact {
		t.Fatalf("unverified-only should be NEEDS_CONTACT, got %v", cn)
	}
}

func TestImportDryRunDoesNotPersistAccounts(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	run, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{DryRun: true})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !run.DryRun {
		t.Fatal("expected dry_run")
	}
	if run.Counts.Creates < 1 {
		t.Fatalf("dry-run should count creates: %+v", run.Counts)
	}
	// accounts map empty of real companies
	acc, _ := repo.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if acc != nil {
		t.Fatal("dry-run must not persist accounts")
	}
}

func TestImportIdempotencySameKeySamePayload(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	opts := ImportOptions{IdempotencyKey: "idem-1"}
	r1, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, opts)
	if xerr != nil {
		t.Fatal(xerr)
	}
	r2, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, opts)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r1.ID != r2.ID {
		t.Fatalf("idempotent reimport should return same run %s vs %s", r1.ID, r2.ID)
	}
}

func TestImportIdempotencyConflictOnDifferentPayload(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	opts := ImportOptions{IdempotencyKey: "idem-2"}
	if _, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, opts); xerr != nil {
		t.Fatal(xerr)
	}
	other := []byte(`{"schema_version":"confenge.outreach.v1","source":{"system":"extra-cli"},"leads":[]}`)
	_, xerr := svc.ImportFromBytes(context.Background(), org, nil, other, opts)
	if xerr == nil {
		t.Fatal("expected conflict")
	}
}

func TestReimportPreservesDNC(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	if _, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{}); xerr != nil {
		t.Fatal(xerr)
	}
	acc, _ := repo.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if acc == nil {
		t.Fatal("missing")
	}
	acc.DoNotContact = true
	acc.QueueState = models.OutreachQueueDoNotContact
	_, _ = repo.UpsertAccount(context.Background(), acc)

	if _, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{}); xerr != nil {
		t.Fatal(xerr)
	}
	again, _ := repo.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if again == nil || !again.DoNotContact {
		t.Fatal("DNC must survive reimport")
	}
}

func TestReimportIdenticalIsUnchanged(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	if _, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{}); xerr != nil {
		t.Fatal(xerr)
	}
	run2, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run2.Counts.Creates != 0 {
		t.Fatalf("second import should not create: %+v", run2.Counts)
	}
	if run2.Counts.Unchanged < 1 {
		t.Fatalf("expected unchanged counts: %+v", run2.Counts)
	}
}

func TestReimportScoreChangeUpdates(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	if _, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{}); xerr != nil {
		t.Fatal(xerr)
	}
	// Mutate score in payload
	feed, _ := ParseFeed(raw)
	feed.Leads[0].Priority.Score = 99
	mut, _ := jsonMarshal(feed)
	run, xerr := svc.ImportFromBytes(context.Background(), org, nil, mut, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run.Counts.Updates < 1 {
		t.Fatalf("score change should update: %+v", run.Counts)
	}
}

func TestMultiTenantIsolation(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	orgA, orgB := uuid.New(), uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	if _, xerr := svc.ImportFromBytes(context.Background(), orgA, nil, raw, ImportOptions{}); xerr != nil {
		t.Fatal(xerr)
	}
	accB, _ := repo.GetAccountByCNPJ(context.Background(), orgB, "11222333000181")
	if accB != nil {
		t.Fatal("org B must not see org A accounts")
	}
	accA, _ := repo.GetAccountByCNPJ(context.Background(), orgA, "11222333000181")
	if accA == nil {
		t.Fatal("org A should have account")
	}
}

func TestDisabledServiceRejects(t *testing.T) {
	repo := newMemRepo()
	cfg := Config{Enabled: false}
	svc := NewService(cfg, repo, nil)
	_, xerr := svc.ImportFromBytes(context.Background(), uuid.New(), nil, []byte(`{}`), ImportOptions{})
	if xerr == nil {
		t.Fatal("disabled must reject")
	}
}

func TestLegacyImportWorks(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "legacy_leads_array.json")
	run, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run.Counts.Creates < 2 {
		t.Fatalf("legacy creates: %+v errs=%+v", run.Counts, run.Errors)
	}
	if run.SourceGeneratedAt != nil {
		t.Fatalf("legacy import must not manufacture source freshness: %v", run.SourceGeneratedAt)
	}
	noContact, _ := repo.GetAccountByCNPJ(context.Background(), org, "55444333000122")
	if noContact == nil || noContact.QueueState != models.OutreachQueueTargetFitSuppressed ||
		noContact.TargetFitSuppressionReason != TargetFitReasonMissing {
		t.Fatalf("legacy no-contact company: %+v", noContact)
	}
}

func TestInvalidFeedRejected(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	_, xerr := svc.ImportFromBytes(context.Background(), uuid.New(), nil, []byte(`{not json`), ImportOptions{})
	if xerr == nil {
		t.Fatal("expected invalid feed error")
	}
}

func TestEvidenceAddedOnReimport(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo)
	org := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	if _, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{}); xerr != nil {
		t.Fatal(xerr)
	}
	feed, _ := ParseFeed(raw)
	feed.Leads[0].Evidence = append(feed.Leads[0].Evidence, FeedEvidence{
		ID:             "ev-acme-2",
		Title:          "Nova evidencia",
		EpistemicClass: models.OutreachEpistemicConfirmedFact,
	})
	// Also change a machine field so content hash moves (evidence is in hash)
	mut, _ := jsonMarshal(feed)
	run, xerr := svc.ImportFromBytes(context.Background(), org, nil, mut, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run.Counts.EvidenceAdded < 1 {
		t.Fatalf("expected new evidence: %+v", run.Counts)
	}
}

// local helper avoiding import cycle noise
func jsonMarshal(v any) ([]byte, error) {
	return marshalJSON(v)
}

func (m *memRepo) CountByActivationState(ctx context.Context, orgID uuid.UUID, now time.Time) (*repository.OutreachActivationCounts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := &repository.OutreachActivationCounts{}
	for _, a := range m.byID {
		if a.OrganizationID != orgID {
			continue
		}
		out.Total++
		switch a.ActivationState {
		case ActivationActionableNow:
			out.ActionableNow++
			if IsOutboundDue(a, now) {
				if a.QueueState == models.OutreachQueueNeedsContact {
					out.NeedsContactDue++
				}
				for _, c := range m.cands[a.ID] {
					if RequireEmailOutbound(a, &c) == nil {
						out.ActionableDueNow++
						break
					}
				}
			}
		case ActivationSuppressed:
			out.Suppressed++
		case ActivationResearchRequired:
			out.ResearchRequired++
		default:
			out.Watch++
		}
	}
	return out, nil
}

func (m *memRepo) GetFeedSyncState(ctx context.Context, orgID uuid.UUID) (*models.OutreachFeedSyncState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.feedSync == nil {
		return nil, nil
	}
	st := m.feedSync[orgID]
	if st == nil {
		return nil, nil
	}
	cp := *st
	return &cp, nil
}

func (m *memRepo) UpsertFeedSyncState(ctx context.Context, st *models.OutreachFeedSyncState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.feedSync == nil {
		m.feedSync = map[uuid.UUID]*models.OutreachFeedSyncState{}
	}
	cp := *st
	m.feedSync[st.OrganizationID] = &cp
	return nil
}

func (m *memRepo) ListPilotMemberships(ctx context.Context, orgID uuid.UUID, cohortID string) ([]models.OutreachPilotMembership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]models.OutreachPilotMembership, 0)
	for _, membership := range m.pilotMemberships {
		if membership.OrganizationID == orgID && membership.CohortID == cohortID {
			result = append(result, *membership)
		}
	}
	return result, nil
}

func (m *memRepo) ClaimPilotOperation(ctx context.Context, orgID uuid.UUID, operationKey, requestHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := orgID.String() + "|" + operationKey
	if existing, ok := m.pilotOperations[key]; ok && existing != requestHash {
		return repository.ErrPilotIdempotencyConflict
	}
	m.pilotOperations[key] = requestHash
	return nil
}

func (m *memRepo) ReservePilotSlot(_ context.Context, orgID uuid.UUID, cohortID string, accountID uuid.UUID, cnpj14 string, capacity int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := orgID.String() + "|" + cohortID + "|"
	key := prefix + accountID.String()
	count := 0
	for existingKey := range m.pilotSlots {
		if strings.HasPrefix(existingKey, prefix) {
			count++
		}
	}
	if existingCNPJ, ok := m.pilotSlots[key]; ok {
		if existingCNPJ != cnpj14 {
			return count, fmt.Errorf("pilot slot CNPJ conflict")
		}
		return count, nil
	}
	if count >= capacity {
		return count, repository.ErrPilotCapacityReached
	}
	m.pilotSlots[key] = cnpj14
	return count + 1, nil
}

func (m *memRepo) ReleasePilotSlot(_ context.Context, orgID uuid.UUID, cohortID string, accountID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, membership := range m.pilotMemberships {
		if membership.OrganizationID == orgID && membership.CohortID == cohortID && membership.AccountID == accountID {
			return nil
		}
	}
	delete(m.pilotSlots, orgID.String()+"|"+cohortID+"|"+accountID.String())
	return nil
}

func (m *memRepo) ClaimPilotMembership(ctx context.Context, membership *models.OutreachPilotMembership, capacity int) (*models.OutreachPilotMembership, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, existing := range m.pilotMemberships {
		if existing.OrganizationID != membership.OrganizationID || existing.CohortID != membership.CohortID {
			continue
		}
		count++
		if existing.AccountID == membership.AccountID {
			copy := *existing
			return &copy, count, nil
		}
	}
	if count >= capacity {
		return nil, count, repository.ErrPilotCapacityReached
	}
	copy := *membership
	if copy.ID == uuid.Nil {
		copy.ID = uuid.New()
	}
	copy.CreatedAt, copy.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	m.pilotMemberships[copy.ID] = &copy
	return &copy, count + 1, nil
}

func (m *memRepo) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.advLocks == nil {
		m.advLocks = map[int64]bool{}
	}
	if m.advLocks[key] {
		return false, nil
	}
	m.advLocks[key] = true
	return true, nil
}

func (m *memRepo) AdvisoryUnlock(ctx context.Context, key int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.advLocks != nil {
		delete(m.advLocks, key)
	}
	return nil
}

func (m *memRepo) ensureActions() {
	if m.actions == nil {
		m.actions = map[uuid.UUID]*models.OutreachCommercialAction{}
	}
	if m.actionByIdem == nil {
		m.actionByIdem = map[string]*models.OutreachCommercialAction{}
	}
}

func (m *memRepo) UpsertCommercialAction(_ context.Context, a *models.OutreachCommercialAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureActions()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	a.UpdatedAt = time.Now().UTC()
	cp := *a
	m.actions[a.ID] = &cp
	if a.IdempotencyKey != "" {
		m.actionByIdem[a.OrganizationID.String()+"|"+a.IdempotencyKey] = &cp
	}
	return nil
}

func (m *memRepo) GetCommercialAction(_ context.Context, orgID, id uuid.UUID) (*models.OutreachCommercialAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureActions()
	a := m.actions[id]
	if a == nil || a.OrganizationID != orgID {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memRepo) GetCommercialActionByIdempotency(_ context.Context, orgID uuid.UUID, key string) (*models.OutreachCommercialAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureActions()
	a := m.actionByIdem[orgID.String()+"|"+key]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memRepo) ListCommercialActions(_ context.Context, orgID, accountID uuid.UUID, openOnly bool, limit int) ([]models.OutreachCommercialAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureActions()
	var out []models.OutreachCommercialAction
	for _, a := range m.actions {
		if a.OrganizationID != orgID {
			continue
		}
		if accountID != uuid.Nil && a.AccountID != accountID {
			continue
		}
		if openOnly {
			switch a.State {
			case models.ActionStatePlanned, models.ActionStateReady, models.ActionStateInProgress, models.ActionStateNeedsFollowup:
			default:
				continue
			}
		}
		out = append(out, *a)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
