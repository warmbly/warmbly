package jobs

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type repairInbox struct {
	repository.UniboxRepository
	events []models.JobEventNewEmail
	pages  []repository.CampaignReplyRepairPage
	since  time.Time
}

func (r *repairInbox) ListUnprocessedCampaignReplies(_ context.Context, since time.Time, afterID uuid.UUID, limit int) (repository.CampaignReplyRepairPage, error) {
	r.since = since
	if len(r.pages) > 0 {
		page := r.pages[0]
		r.pages = r.pages[1:]
		return page, nil
	}
	// Ordered by id like the query, so the cursor is deterministic.
	var out []models.JobEventNewEmail
	for _, e := range r.events {
		if e.Message.ID.String() > afterID.String() {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Message.ID.String() < out[j].Message.ID.String() })
	out = out[:min(len(out), limit)]
	page := repository.CampaignReplyRepairPage{Events: out, NextCursor: afterID, Done: len(out) < limit}
	if len(out) > 0 {
		page.NextCursor = out[len(out)-1].Message.ID
	}
	return page, nil
}

type repairAdvanced struct {
	replyRecordingAdvanced
	failOn uuid.UUID
	seen   []uuid.UUID
}

func (s *repairAdvanced) ProcessIncomingReply(_ context.Context, _ uuid.UUID, m *models.EmailMessageStoreData) *errx.Error {
	s.seen = append(s.seen, m.ID)
	if m.ID == s.failOn {
		return errx.InternalError()
	}
	return nil
}

// Every unclaimed reply is re-offered to the same handler the live path uses,
// a failure on one does not stop the rest, and the cursor lands on the last
// message so the next batch continues rather than repeats.
func TestIncomingReplyRepairReoffersEveryUnclaimedReply(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	var events []models.JobEventNewEmail
	for _, id := range ids {
		events = append(events, models.JobEventNewEmail{UserID: uuid.New(), Message: &models.EmailMessageStoreData{ID: id, EmailID: uuid.New(), MessageID: "<" + id.String() + "@test>"}})
	}
	inbox := &repairInbox{events: events}
	adv := &repairAdvanced{failOn: ids[1]}
	s := &JobsService{UniboxRepository: inbox, AdvancedService: adv}

	next, done, err := s.repairIncomingReplyBatch(context.Background(), uuid.Nil)
	if err != nil || !done {
		t.Fatalf("batch: next=%v done=%v err=%v", next, done, err)
	}
	if len(adv.seen) != 3 {
		t.Fatalf("re-offered %d replies, want all 3 (a failure must not stop the sweep)", len(adv.seen))
	}
	// The cursor is the highest id seen, whichever order the fake returned.
	var last uuid.UUID
	for _, id := range ids {
		if id.String() > last.String() {
			last = id
		}
	}
	if next != last {
		t.Fatalf("cursor = %v, want the last message %v", next, last)
	}
	if time.Since(inbox.since) > replyRepairWindow+time.Minute || time.Since(inbox.since) < replyRepairWindow-time.Minute {
		t.Fatalf("asked since %v, want the repair window", inbox.since)
	}
}

func TestIncomingReplyRepairNeedsBothCollaborators(t *testing.T) {
	// Neither a nil inbox nor a nil advanced service may panic the consumer.
	(&JobsService{}).StartIncomingReplyRepair(context.Background())
	(&JobsService{UniboxRepository: &repairInbox{}}).StartIncomingReplyRepair(context.Background())
	_ = errors.New
}

func TestIncomingReplyRepairKeepsScanningWhenAFullRawPageHasNoMatches(t *testing.T) {
	first := uuid.New()
	second := uuid.New()
	event := models.JobEventNewEmail{Message: &models.EmailMessageStoreData{ID: second, EmailID: uuid.New()}}
	inbox := &repairInbox{pages: []repository.CampaignReplyRepairPage{
		{NextCursor: first},
		{Events: []models.JobEventNewEmail{event}, NextCursor: second, Done: true},
	}}
	adv := &repairAdvanced{}
	s := &JobsService{UniboxRepository: inbox, AdvancedService: adv}

	next, done, err := s.repairIncomingReplyBatch(context.Background(), uuid.Nil)
	if err != nil || done || next != first || len(adv.seen) != 0 {
		t.Fatalf("first raw page: next=%v done=%t seen=%d err=%v", next, done, len(adv.seen), err)
	}
	next, done, err = s.repairIncomingReplyBatch(context.Background(), next)
	if err != nil || !done || next != second || len(adv.seen) != 1 || adv.seen[0] != second {
		t.Fatalf("second raw page: next=%v done=%t seen=%v err=%v", next, done, adv.seen, err)
	}
}
