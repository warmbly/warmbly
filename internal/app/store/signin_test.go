package store

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

type stubLinks struct {
	hint, back string
	ok         bool
	connected  int
}

func (s *stubLinks) LoginHint(context.Context, uuid.UUID) string { return s.hint }
func (s *stubLinks) Connected(context.Context, uuid.UUID, *models.Email) {
	s.connected++
}
func (s *stubLinks) ReturnURL(context.Context, uuid.UUID, string) (string, bool) { return s.back, s.ok }

func TestPartnerLinksChainAsksEachInTurn(t *testing.T) {
	storeLinks, partner := &stubLinks{}, &stubLinks{hint: "a@b.com", back: "https://p.example/r", ok: true}
	c := PartnerLinksChain{storeLinks, partner}
	ctx, id := context.Background(), uuid.New()
	if got := c.LoginHint(ctx, id); got != "a@b.com" {
		t.Fatalf("hint %q", got)
	}
	if u, ok := c.ReturnURL(ctx, id, "connected"); !ok || u != "https://p.example/r" {
		t.Fatalf("return %q %v", u, ok)
	}
	c.Connected(ctx, id, &models.Email{})
	if storeLinks.connected != 1 || partner.connected != 1 {
		t.Fatalf("connected %d %d", storeLinks.connected, partner.connected)
	}
	storeLinks.hint = "store@b.com"
	if got := c.LoginHint(ctx, id); got != "store@b.com" {
		t.Fatalf("store hint first: %q", got)
	}
}

func TestOAuthMailboxesConnectOnlyThroughTheirSignIn(t *testing.T) {
	m := &graphkit.Mailbox{Provider: graphkit.ProviderGoogle, Status: graphkit.MailboxActive, Connection: graphkit.Connection{Method: graphkit.ConnectOAuth}}
	if !waitsForSignIn(m) {
		t.Fatal("an oauth mailbox that is not ready waits for its sign-in")
	}
	m.Status = graphkit.MailboxProvisioning
	if !waitsForSignIn(m) {
		t.Fatal("an oauth mailbox the vendor still sets up waits for its sign-in")
	}
	m.Status = graphkit.MailboxActive
	m.Connection.Ready = true
	if waitsForSignIn(m) || connectable(&models.StoreMailbox{}, m) {
		t.Fatal("a ready oauth mailbox neither waits nor auto-connects")
	}
	m.Connection.Method = graphkit.ConnectDelegation
	if !connectable(&models.StoreMailbox{}, m) {
		t.Fatal("delegation connects as before")
	}
}
