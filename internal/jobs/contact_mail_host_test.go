package jobs

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/repository"
)

type mailHostDNS struct {
	mx    map[string][]string
	mu    sync.Mutex
	calls map[string]int
}

func (d *mailHostDNS) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	d.mu.Lock()
	d.calls[name]++
	d.mu.Unlock()
	if name == "flaky.example" {
		return nil, &net.DNSError{Err: "server misbehaving", Name: name, IsTemporary: true}
	}
	hosts, ok := d.mx[name]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	out := make([]*net.MX, len(hosts))
	for i, h := range hosts {
		out[i] = &net.MX{Host: h + ".", Pref: 10}
	}
	return out, nil
}

func (d *mailHostDNS) LookupTXT(context.Context, string) ([]string, error) {
	return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
}

type mailHostStore struct {
	pending []repository.ContactMailHostPending
	got     []repository.ContactMailHostResult
}

func (s *mailHostStore) ListMailHostPending(context.Context, int) ([]repository.ContactMailHostPending, error) {
	p := s.pending
	s.pending = nil
	return p, nil
}

func (s *mailHostStore) SetContactMailHosts(_ context.Context, r []repository.ContactMailHostResult) ([]uuid.UUID, error) {
	s.got = append(s.got, r...)
	return nil, nil
}

func TestContactMailHostSweep(t *testing.T) {
	store := &mailHostStore{pending: []repository.ContactMailHostPending{
		{ID: uuid.New(), Email: "dana@gmail.com"},
		{ID: uuid.New(), Email: "a@acme.example"},
		{ID: uuid.New(), Email: "b@acme.example"},
		{ID: uuid.New(), Email: "c@flaky.example"},
		{ID: uuid.New(), Email: "d@nomail.example"},
	}}
	dns := &mailHostDNS{mx: map[string][]string{"acme.example": {"aspmx.l.google.com"}}, calls: map[string]int{}}
	j := NewContactMailHostSweep(store, nil, nil)
	j.resolver = dns
	if err := j.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		host, esp string
		transient bool
	}{
		{"gmail", "gmail", false},
		{"google_workspace", "gmail", false},
		{"google_workspace", "gmail", false},
		{"", "", true},
		{"", "", false},
	}
	if len(store.got) != len(want) {
		t.Fatalf("got %d results, want %d", len(store.got), len(want))
	}
	for i, w := range want {
		g := store.got[i]
		if g.MailHost != w.host || g.ESP != w.esp || g.Transient != w.transient {
			t.Errorf("%s: got host=%q esp=%q transient=%v, want %q %q %v", g.Email, g.MailHost, g.ESP, g.Transient, w.host, w.esp, w.transient)
		}
	}
	if dns.calls["acme.example"] != 1 {
		t.Errorf("acme.example looked up %d times, want once per pass", dns.calls["acme.example"])
	}
	if dns.calls["gmail.com"] != 0 {
		t.Errorf("a consumer domain dialled DNS")
	}
}
