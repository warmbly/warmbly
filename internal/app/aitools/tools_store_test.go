package aitools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/store"
)

// moneyStore records which store calls move money: a charge, a renewal, or a change to what is billed.
type moneyStore struct{ moved []string }

func (m *moneyStore) Status(context.Context, uuid.UUID) (*store.Status, error) {
	return &store.Status{}, nil
}
func (m *moneyStore) Search(context.Context, string, []string, bool) (*store.SearchResult, error) {
	return &store.SearchResult{}, nil
}
func (m *moneyStore) ListOrders(context.Context, uuid.UUID, string, int) (*store.Page[store.Order], error) {
	return &store.Page[store.Order]{}, nil
}
func (m *moneyStore) GetOrder(context.Context, uuid.UUID, uuid.UUID) (*store.Order, error) {
	return &store.Order{}, nil
}
func (m *moneyStore) ListDomains(context.Context, uuid.UUID, string, int) (*store.Page[store.Domain], error) {
	return &store.Page[store.Domain]{}, nil
}
func (m *moneyStore) GetDomain(context.Context, uuid.UUID, uuid.UUID) (*store.Domain, error) {
	return &store.Domain{}, nil
}
func (m *moneyStore) ListMailboxes(context.Context, uuid.UUID, string, int) (*store.Page[store.Mailbox], error) {
	return &store.Page[store.Mailbox]{}, nil
}
func (m *moneyStore) CreateQuote(context.Context, uuid.UUID, store.QuoteRequest) (*store.Quote, error) {
	return &store.Quote{}, nil
}
func (m *moneyStore) PlaceOrder(context.Context, uuid.UUID, uuid.UUID, store.OrderRequest) (*store.Order, error) {
	m.moved = append(m.moved, "PlaceOrder")
	return &store.Order{}, nil
}
func (m *moneyStore) PatchDomain(_ context.Context, _, _ uuid.UUID, p store.DomainPatch) (*store.Domain, error) {
	if p.AutoRenew != nil {
		m.moved = append(m.moved, "PatchDomain.AutoRenew")
	}
	return &store.Domain{}, nil
}
func (m *moneyStore) RenewNow(context.Context, uuid.UUID, uuid.UUID) (*store.Domain, error) {
	m.moved = append(m.moved, "RenewNow")
	return &store.Domain{}, nil
}
func (m *moneyStore) CancelMailbox(context.Context, uuid.UUID, uuid.UUID) (*store.Mailbox, error) {
	m.moved = append(m.moved, "CancelMailbox")
	return &store.Mailbox{}, nil
}
func (m *moneyStore) ResumeMailbox(context.Context, uuid.UUID, uuid.UUID) (*store.Mailbox, error) {
	m.moved = append(m.moved, "ResumeMailbox")
	return &store.Mailbox{}, nil
}
func (m *moneyStore) AddDNSRecord(context.Context, uuid.UUID, uuid.UUID, store.DNSRecordInput) (*store.DNSRecord, error) {
	return &store.DNSRecord{}, nil
}
func (m *moneyStore) DeleteDNSRecord(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

// Every store tool that reaches a call moving money asks the person each time and needs a fresh confirmation.
func TestStoreToolsThatMoveMoneyAlwaysAsk(t *testing.T) {
	r := NewRegistry()
	RegisterStoreTools(r, &moneyStore{}, nil)
	registered := map[string]bool{}
	for _, name := range r.order {
		if strings.Contains(name, "store") {
			registered[name] = true
		}
	}
	for _, name := range StoreToolNames {
		if !registered[name] {
			t.Errorf("%s is in StoreToolNames but not registered", name)
		}
		delete(registered, name)
	}
	for name := range registered {
		t.Errorf("%s is registered but missing from StoreToolNames", name)
	}

	id := uuid.NewString()
	args, _ := json.Marshal(map[string]any{
		"quote_id": id, "expected_due_now_micros": 1, "domain_id": id, "mailbox_id": id, "order_id": id, "record_id": id,
		"auto_renew": true, "query": "acme", "items": []any{}, "type": "TXT", "name": "@", "content": "v=x",
	})
	reached := map[string]bool{}
	for _, name := range StoreToolNames {
		tool, ok := r.Get(name)
		if !ok {
			continue
		}
		rec := &moneyStore{}
		local := NewRegistry()
		RegisterStoreTools(local, rec, nil)
		lt, _ := local.Get(name)
		if _, err := lt.Handler(context.Background(), Invocation{OrgID: uuid.New(), UserID: uuid.New()}, args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(rec.moved) == 0 {
			continue
		}
		for _, m := range rec.moved {
			reached[m] = true
		}
		if !tool.AlwaysAsk || !tool.FreshAuth {
			t.Errorf("%s calls %v but AlwaysAsk=%v FreshAuth=%v", name, rec.moved, tool.AlwaysAsk, tool.FreshAuth)
		}
	}
	// Every money call is reached by some tool, so the recording above cannot go stale.
	for _, m := range []string{"PlaceOrder", "PatchDomain.AutoRenew", "RenewNow", "CancelMailbox", "ResumeMailbox"} {
		if !reached[m] {
			t.Errorf("no store tool reaches %s", m)
		}
	}
}
