package advanced

import (
	"context"

	"github.com/google/uuid"
)

// LabelThread additively applies the given category labels to a unibox
// conversation, on behalf of the thread's owning user. Backs the "label_email"
// automation action. Best-effort: empty input or a missing labeler is a no-op,
// and categories not owned by userID are silently ignored by the repository.
func (s *service) LabelThread(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error {
	if s.uniboxRepo == nil || threadID == "" || len(categoryIDs) == 0 {
		return nil
	}
	return s.uniboxRepo.AddThreadLabels(ctx, userID, threadID, categoryIDs)
}

// LabelLatestThreadForContact resolves the contact's most recent conversation in
// userID's unibox and labels it. Backs the "label_email" campaign step action,
// which knows the contact but not the thread id (off a reply branch the most
// recent thread IS the reply). Returns the labeled thread id, or "" when the
// contact has no conversation yet (a logged no-op for the caller).
func (s *service) LabelLatestThreadForContact(ctx context.Context, userID uuid.UUID, contactEmail string, categoryIDs []uuid.UUID) (string, error) {
	if s.uniboxRepo == nil || contactEmail == "" || len(categoryIDs) == 0 {
		return "", nil
	}
	threadID, err := s.uniboxRepo.LatestThreadIDForContact(ctx, userID, contactEmail)
	if err != nil {
		return "", err
	}
	if threadID == "" {
		return "", nil
	}
	return threadID, s.uniboxRepo.AddThreadLabels(ctx, userID, threadID, categoryIDs)
}
