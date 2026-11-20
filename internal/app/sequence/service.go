package sequence

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type SequenceService interface {
	Create(ctx context.Context, orgID, campaignID string) (*models.Sequence, *errx.Error)
	Get(ctx context.Context, orgID, campaignID string) ([]models.Sequence, *errx.Error)
	Update(ctx context.Context, orgID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error)
	UpdateLayout(ctx context.Context, orgID, campaignID string, positions []models.SequencePosition) *errx.Error
	Delete(ctx context.Context, orgID, campaignID, sequenceID string) *errx.Error
	// ReorderSteps repositions the campaign's steps to the given order and
	// reconnects the catch-all chain so it follows the new order, preserving
	// every conditional branch. It rejects an order that is not a permutation of
	// the campaign's steps, or that would create a routing cycle.
	ReorderSteps(ctx context.Context, orgID, campaignID string, order []string) (*models.Sequence, *errx.Error)
}

type sequenceService struct {
	sequenceRepository repository.SequenceRepository
	attachmentRepo     repository.AttachmentRepository
	storage            storage.Store
}

func NewService(sequenceRepository repository.SequenceRepository) SequenceService {
	return &sequenceService{
		sequenceRepository: sequenceRepository,
	}
}

// AttachmentAware is implemented by the sequence service so main can hand it
// the attachment repository and object store. Deleting a step cascades its
// attachment rows away, so without these the files scoped to that step leave
// their bytes in storage forever, still counted against the org's quota.
type AttachmentAware interface {
	WireAttachments(repo repository.AttachmentRepository, store storage.Store)
}

func (s *sequenceService) WireAttachments(repo repository.AttachmentRepository, store storage.Store) {
	s.attachmentRepo, s.storage = repo, store
}

// ReorderSteps repositions the campaign's steps to the given order and
// reconnects the catch-all chain so it follows the new order, preserving every
// conditional branch. Routing is purely branch-based (position only picks the
// entry step and orders the canvas), so a linear chain only reorders when its
// catch-all links follow the new order; a branching chain keeps its conditional
// branches and only its fall-through catch-all is re-pointed. The order must be
// a permutation of the campaign's steps, and must not create a routing cycle.
func (s *sequenceService) ReorderSteps(ctx context.Context, orgID, campaignID string, order []string) (*models.Sequence, *errx.Error) {
	steps, xerr := s.sequenceRepository.Get(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	if len(steps) == 0 {
		return nil, errx.ErrNotFound
	}

	byID := make(map[string]models.Sequence, len(steps))
	for _, st := range steps {
		byID[st.ID.String()] = st
	}

	seen := make(map[string]bool, len(order))
	updates := make([]repository.SequenceReorder, 0, len(order))
	for i, idStr := range order {
		if seen[idStr] {
			return nil, errx.ErrSequenceOrder
		}
		st, ok := byID[idStr]
		if !ok {
			return nil, errx.ErrNotFound
		}
		seen[idStr] = true

		var nextID *uuid.UUID
		if i+1 < len(order) {
			next := byID[order[i+1]]
			nextID = &next.ID
		}
		conditions, cerr := reconnectCatchAll(st.Conditions, nextID)
		if cerr != nil {
			return nil, cerr
		}
		raw, merr := json.Marshal(conditions)
		if merr != nil {
			return nil, errx.InternalError()
		}
		updates = append(updates, repository.SequenceReorder{ID: st.ID, Position: i + 1, Conditions: raw})
	}
	if len(seen) != len(steps) {
		// The order omitted a step that exists in the campaign.
		return nil, errx.ErrSequenceOrder
	}

	if xerr := reorderCreatesCycle(steps, updates); xerr != nil {
		return nil, xerr
	}

	if xerr := s.sequenceRepository.Reorder(ctx, orgID, campaignID, updates); xerr != nil {
		return nil, xerr
	}

	// The entry step is the first step in the new order.
	entry := byID[order[0]]
	return &entry, nil
}

// reconnectCatchAll returns the step's branches with every conditional branch
// (one that has conditions) preserved and a single unconditional catch-all that
// points to nextID — or no catch-all when nextID is nil (the last step). This is
// what lets a reorder re-establish the linear "go there next" chain without
// disturbing the user's engagement branches.
func reconnectCatchAll(raw json.RawMessage, nextID *uuid.UUID) (*models.BranchConditions, *errx.Error) {
	bc := &models.BranchConditions{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, bc); err != nil {
			return nil, errx.ErrSequenceBranch
		}
	}

	var catchAllBranchID string
	conditional := make([]models.Branch, 0, len(bc.Branches))
	for _, br := range bc.Branches {
		if len(br.Conditions) > 0 {
			conditional = append(conditional, br)
		} else if catchAllBranchID == "" {
			// Reuse the existing catch-all's stable client-supplied id so the
			// editor's diffing does not see a brand-new branch.
			catchAllBranchID = br.BranchID
		}
	}
	if nextID != nil {
		conditional = append(conditional, models.Branch{
			BranchID:         catchAllBranchID,
			TargetSequenceID: nextID,
			Conditions:       nil,
		})
	}
	bc.Branches = conditional
	return bc, nil
}

// reorderCreatesCycle reports whether writing the reordered positions and their
// reconnected catch-all chain would make the branch graph contain a cycle. The
// catch-all chain strictly increases in position, so a cycle can only form from
// a preserved conditional branch pointing "backwards"; such a reorder is
// refused rather than left to loop a contact forever at schedule time.
func reorderCreatesCycle(steps []models.Sequence, updates []repository.SequenceReorder) *errx.Error {
	positionOf := make(map[string]int, len(updates))
	targets := make(map[string][]uuid.UUID, len(updates))
	for _, u := range updates {
		positionOf[u.ID.String()] = u.Position
		var cond models.BranchConditions
		if len(u.Conditions) > 0 {
			if err := json.Unmarshal(u.Conditions, &cond); err != nil {
				return errx.InternalError()
			}
		}
		for _, br := range cond.Branches {
			if br.TargetSequenceID != nil {
				targets[u.ID.String()] = append(targets[u.ID.String()], *br.TargetSequenceID)
			}
		}
	}

	const (
		notVisited = 0
		inProgress = 1
		done       = 2
	)
	state := make(map[string]int, len(steps))

	var visit func(id string) bool
	visit = func(id string) bool {
		state[id] = inProgress
		for _, next := range targets[id] {
			nextID := next.String()
			if state[nextID] == inProgress {
				return true
			}
			if state[nextID] == done {
				continue
			}
			if visit(nextID) {
				return true
			}
		}
		state[id] = done
		return false
	}

	for _, st := range steps {
		if state[st.ID.String()] == done {
			continue
		}
		if visit(st.ID.String()) {
			return errx.ErrSequenceCycle
		}
	}
	return nil
}
