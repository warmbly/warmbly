package sequence

import (
	"context"
	"encoding/json"
	"strings"

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
	// ReorderSteps puts a straight-through sequence that has not sent yet in a new order.
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

// ReorderSteps puts a straight-through sequence in a new order. Each position
// keeps its wait, so a move changes which step goes out at each point, not when.
// The repository refuses once the campaign has sent a step.
func (s *sequenceService) ReorderSteps(ctx context.Context, orgID, campaignID string, order []string) (*models.Sequence, *errx.Error) {
	plan := func(steps []models.Sequence) ([]repository.SequenceReorder, *errx.Error) {
		return buildReorder(steps, order)
	}
	return s.sequenceRepository.Reorder(ctx, orgID, campaignID, plan)
}

// buildReorder turns a permutation of steps (given in position order) into the rows to write.
func buildReorder(steps []models.Sequence, order []string) ([]repository.SequenceReorder, *errx.Error) {
	if len(steps) == 0 {
		return nil, errx.ErrNotFound
	}
	byID := make(map[string]models.Sequence, len(steps))
	for _, st := range steps {
		byID[st.ID.String()] = st
	}
	if len(order) != len(steps) {
		return nil, errx.ErrSequenceOrder
	}
	ordered := make([]models.Sequence, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, id := range order {
		st, ok := byID[id]
		if !ok || seen[id] {
			return nil, errx.ErrSequenceOrder
		}
		seen[id] = true
		ordered = append(ordered, st)
	}

	branchIDs, ok := linearChain(steps)
	if !ok {
		return nil, errx.ErrSequenceOrderBranched
	}
	for _, st := range ordered {
		if st.Kind == "" || st.Kind == "email" {
			if strings.TrimSpace(st.Subject) == "" {
				return nil, errx.ErrSequenceOrderSubject
			}
			break
		}
	}

	updates := make([]repository.SequenceReorder, len(ordered))
	for i, st := range ordered {
		bc := models.BranchConditions{}
		if i+1 < len(ordered) {
			next := ordered[i+1].ID
			bc.Branches = []models.Branch{{BranchID: branchIDs[st.ID], TargetSequenceID: &next}}
		}
		raw, err := json.Marshal(bc)
		if err != nil {
			return nil, errx.InternalError()
		}
		updates[i] = repository.SequenceReorder{ID: st.ID, Position: i + 1, WaitAfter: steps[i].WaitAfter, Conditions: raw}
	}
	return updates, nil
}

// linearChain reports whether steps, in position order, run straight through:
// every step but the last has exactly one unconditional connection, to the next
// step, and the last has none. It returns each step's connection id so a rewrite keeps it.
func linearChain(steps []models.Sequence) (map[uuid.UUID]string, bool) {
	ids := make(map[uuid.UUID]string, len(steps))
	for i, st := range steps {
		var bc models.BranchConditions
		if len(st.Conditions) > 0 {
			if err := json.Unmarshal(st.Conditions, &bc); err != nil {
				return nil, false
			}
		}
		if i == len(steps)-1 {
			for _, br := range bc.Branches {
				if len(br.Conditions) > 0 || br.TargetSequenceID != nil {
					return nil, false
				}
			}
			continue
		}
		if len(bc.Branches) != 1 {
			return nil, false
		}
		br := bc.Branches[0]
		if len(br.Conditions) > 0 || br.TargetSequenceID == nil || *br.TargetSequenceID != steps[i+1].ID {
			return nil, false
		}
		ids[st.ID] = br.BranchID
	}
	return ids, true
}
