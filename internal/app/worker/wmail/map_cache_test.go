package wmail

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

type countingMap struct {
	rows  map[string]repository.EmailMessageData
	calls int
	// during runs after the next read's answer is taken, standing in for a concurrent write.
	during func()
}

func (m *countingMap) read() {
	m.calls++
	if f := m.during; f != nil {
		m.during = nil
		f()
	}
}

func (m *countingMap) Add(_ context.Context, d repository.EmailMessageData) error {
	m.rows[d.MessageID] = d
	return nil
}

func (m *countingMap) Get(_ context.Context, _, _ uuid.UUID, id string) (*repository.EmailMessageData, error) {
	d, ok := m.rows[id]
	m.read()
	if ok {
		return &d, nil
	}
	return nil, nil
}

func (m *countingMap) GetMany(_ context.Context, _, _ uuid.UUID, ids []string) (map[string]*repository.EmailMessageData, error) {
	m.read()
	out := map[string]*repository.EmailMessageData{}
	for _, id := range ids {
		if d, ok := m.rows[id]; ok {
			out[id] = &d
		}
	}
	return out, nil
}

func (m *countingMap) Del(_ context.Context, _, _ uuid.UUID, id string, _ uuid.UUID) error {
	delete(m.rows, id)
	return nil
}

func TestCachedMessageMapAsksOncePerIDUntilAWrite(t *testing.T) {
	ctx := context.Background()
	user, mailbox := uuid.New(), uuid.New()
	internal := uuid.NewString()
	inner := &countingMap{rows: map[string]repository.EmailMessageData{
		"known": {MessageID: "known", ID: internal, ThreadID: "t1"},
	}}
	c := newCachedMessageMap(inner)
	page := []string{"known", "unknown"}

	for range 3 {
		got, err := repository.GetEmailMessageMaps(ctx, c, user, mailbox, page)
		if err != nil {
			t.Fatal(err)
		}
		if got["known"] == nil || got["known"].ID != internal || got["known"].ThreadID != "t1" || got["unknown"] != nil {
			t.Fatalf("answers = %+v", got)
		}
	}
	if d, _ := c.Get(ctx, user, mailbox, "unknown"); d != nil || inner.calls != 1 {
		t.Fatalf("re-walked page asked the control plane %d times, want 1", inner.calls)
	}

	// A message stored since is known on the next walk, and a removed one is not.
	if err := c.Add(ctx, repository.EmailMessageData{MessageID: "unknown", ID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	if err := c.Del(ctx, user, mailbox, "known", uuid.Nil); err != nil {
		t.Fatal(err)
	}
	got, _ := repository.GetEmailMessageMaps(ctx, c, user, mailbox, page)
	if got["unknown"] == nil || got["known"] != nil {
		t.Fatalf("answers after writes = %+v", got)
	}
}

func TestCachedMessageMapDropsAnAnswerAWriteRacedPast(t *testing.T) {
	ctx := context.Background()
	user, mailbox := uuid.New(), uuid.New()
	inner := &countingMap{rows: map[string]repository.EmailMessageData{}}
	c := newCachedMessageMap(inner)
	inner.during = func() {
		_ = c.Add(ctx, repository.EmailMessageData{MessageID: "moved", ID: uuid.NewString()})
	}
	if d, _ := c.Get(ctx, user, mailbox, "moved"); d != nil {
		t.Fatalf("read before the write saw %+v", d)
	}
	if d, _ := c.Get(ctx, user, mailbox, "moved"); d == nil {
		t.Fatal("a not-found read that raced a write was kept")
	}
}
