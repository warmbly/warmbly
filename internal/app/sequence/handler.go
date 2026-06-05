package sequence

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *sequenceService) Create(ctx context.Context, userID, campaignID string) (*models.Sequence, *errx.Error) {
	return s.sequenceRepository.Create(ctx, userID, campaignID)
}

func (s *sequenceService) Get(ctx context.Context, userID, campaignID string) ([]models.Sequence, *errx.Error) {
	return s.sequenceRepository.Get(ctx, userID, campaignID)
}

func (s *sequenceService) Update(ctx context.Context, userID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	// Cross-step branching validation. Shape (fields/operators/values) is checked
	// in the repository; here we need the full sequence set to verify branch
	// targets point at OTHER steps in the SAME campaign and that the resulting
	// graph contains no cycles.
	if data.Conditions != nil {
		if verr := s.validateBranchTargets(ctx, userID, campaignID, sequenceID, data.Conditions); verr != nil {
			return nil, verr
		}
	}
	return s.sequenceRepository.Update(ctx, userID, campaignID, sequenceID, data)
}

// validateBranchTargets ensures every branch target is a real step in the same
// campaign and that applying this update introduces no branching cycle. The
// graph edges are: each step's branches → their target steps. The step being
// updated uses its NEW conditions; every other step uses its persisted ones.
func (s *sequenceService) validateBranchTargets(ctx context.Context, userID, campaignID, sequenceID string, bc *models.BranchConditions) *errx.Error {
	if bc == nil || len(bc.Branches) == 0 {
		return nil
	}

	updatedID, perr := uuid.Parse(sequenceID)
	if perr != nil {
		return errx.ErrSequenceBranch
	}

	steps, gerr := s.sequenceRepository.Get(ctx, userID, campaignID)
	if gerr != nil {
		return gerr
	}

	valid := make(map[uuid.UUID]bool, len(steps))
	for _, st := range steps {
		valid[st.ID] = true
	}
	// The updated step must itself belong to the campaign.
	if !valid[updatedID] {
		return errx.ErrNotFound
	}

	// Every target must be a real step in the campaign, and may not be the step
	// itself (a self-branch is a trivial cycle).
	for _, b := range bc.Branches {
		if b.TargetSequenceID == nil {
			continue // nil = stop, always valid
		}
		if *b.TargetSequenceID == updatedID {
			return errx.ErrSequenceBranchTo
		}
		if !valid[*b.TargetSequenceID] {
			return errx.ErrSequenceBranchTo
		}
	}

	// Build the branch graph from persisted conditions, then overlay the new
	// conditions for the step being updated, and check for cycles.
	graph := make(map[uuid.UUID][]uuid.UUID, len(steps))
	for _, st := range steps {
		if st.ID == updatedID {
			continue // overlaid below with the new conditions
		}
		if len(st.Conditions) == 0 {
			continue
		}
		var existing models.BranchConditions
		if json.Unmarshal(st.Conditions, &existing) != nil {
			continue
		}
		for _, b := range existing.Branches {
			if b.TargetSequenceID != nil {
				graph[st.ID] = append(graph[st.ID], *b.TargetSequenceID)
			}
		}
	}
	for _, b := range bc.Branches {
		if b.TargetSequenceID != nil {
			graph[updatedID] = append(graph[updatedID], *b.TargetSequenceID)
		}
	}

	if hasCycle(graph) {
		return errx.ErrSequenceBranchTo
	}
	return nil
}

// hasCycle reports whether the directed branch graph contains a cycle, via DFS
// with a three-colour (white/grey/black) marking.
func hasCycle(graph map[uuid.UUID][]uuid.UUID) bool {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	state := make(map[uuid.UUID]int)

	var visit func(n uuid.UUID) bool
	visit = func(n uuid.UUID) bool {
		state[n] = grey
		for _, m := range graph[n] {
			switch state[m] {
			case grey:
				return true // back-edge → cycle
			case white:
				if visit(m) {
					return true
				}
			}
		}
		state[n] = black
		return false
	}

	for n := range graph {
		if state[n] == white {
			if visit(n) {
				return true
			}
		}
	}
	return false
}

func (s *sequenceService) Delete(ctx context.Context, userID, campaignID, sequenceID string) *errx.Error {
	return s.sequenceRepository.Delete(ctx, userID, campaignID, sequenceID)
}
