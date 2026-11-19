package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

func TestQuoteReadsEachMailboxSignatureAndRefusesOneForAnotherAddress(t *testing.T) {
	var q QuoteRequest
	raw := `{"items":[{"type":"mailboxes","domain":"getacme.com","provider":"google","mailboxes":[{"local_part":"matt","first_name":"Matt","last_name":"M","signature":{"html":"<p>Matt M</p>","plain":"Matt M"}}]}]}`
	if err := json.Unmarshal([]byte(raw), &q); err != nil {
		t.Fatal(err)
	}
	if got := q.Signatures["matt@getacme.com"]; got.HTML != "<p>Matt M</p>" || got.Plain != "Matt M" {
		t.Fatalf("signatures = %+v", q.Signatures)
	}
	if _, err := cleanSignatures(map[string]MailboxSignature{"bob@getacme.com": {}}, q.Items); code(err) != CodeInvalidItems {
		t.Fatalf("a signature for an address not bought: %v", err)
	}
}

func TestSignatureIsBoundedLikeAMailboxsOwnAndBlankIsNone(t *testing.T) {
	var q QuoteRequest
	if err := json.Unmarshal([]byte(`{"items":[{"type":"mailboxes","domain":"getacme.com","provider":"google","mailboxes":[{"local_part":"matt","first_name":"Matt","last_name":"M"}]}]}`), &q); err != nil {
		t.Fatal(err)
	}
	long := map[string]MailboxSignature{"matt@getacme.com": {HTML: strings.Repeat("a", config.SignatureHTMLMax+1)}}
	if _, err := cleanSignatures(long, q.Items); code(err) != CodeInvalidItems {
		t.Fatalf("an over-long signature: %v", err)
	}
	got, err := cleanSignatures(map[string]MailboxSignature{"matt@getacme.com": {HTML: "  ", Plain: "\n"}}, q.Items)
	if err != nil || got["matt@getacme.com"] != (MailboxSignature{}) {
		t.Fatalf("a blank signature = %+v, %v", got, err)
	}
}

const (
	adaHTML  = `<p><b>Ada Lovelace</b><br>Head of Growth</p>`
	adaPlain = "Ada Lovelace\nHead of Growth"
)

func signedOrder(domain, provider string, sig MailboxSignature) QuoteRequest {
	req := domainAndMailboxes(domain, provider, 1)
	req.Signatures = map[string]MailboxSignature{"ada@" + domain: sig}
	return req
}

func connectedSigned(t *testing.T, h *harness, mm *memMailboxes, domain, provider string, sig ...MailboxSignature) *models.StoreMailbox {
	t.Helper()
	want := MailboxSignature{HTML: adaHTML, Plain: adaPlain}
	if len(sig) > 0 {
		want = sig[0]
	}
	h.s.Mailboxes = mm
	h.fund(50)
	h.buy(t, signedOrder(domain, provider, want))
	h.settle(t)
	for _, m := range h.repo.mailboxes {
		if strings.HasSuffix(m.Email, "@"+domain) {
			if m.EmailAccountID == nil {
				t.Fatalf("mailbox not connected: %+v", m)
			}
			return cp(m)
		}
	}
	t.Fatal("no mailbox bought")
	return nil
}

func TestSignatureIsSetInWarmblyAndOnTheGmailAccountWhenAGoogleMailboxConnects(t *testing.T) {
	h := newHarness(t)
	mm := &memMailboxes{status: map[uuid.UUID]string{}}
	m := connectedSigned(t, h, mm, "acmeoutreach.com", graphkit.ProviderGoogle)

	if got := mm.signatures[*m.EmailAccountID]; len(got) != 1 || got[0] != adaPlain {
		t.Fatalf("warmbly signatures = %q", got)
	}
	if len(h.delegator.signatures) != 1 || h.delegator.signatures[0] != "ada@acmeoutreach.com "+adaHTML {
		t.Fatalf("gmail signatures = %q", h.delegator.signatures)
	}
	if m.SignatureSetAt == nil || m.SignatureProviderAt == nil {
		t.Fatalf("stamps = %v %v", m.SignatureSetAt, m.SignatureProviderAt)
	}
	// Set once: a later pass leaves an edit made in Warmbly or Gmail alone.
	h.clock.advance(time.Hour)
	h.settle(t)
	if len(mm.signatures[*m.EmailAccountID]) != 1 || len(h.delegator.signatures) != 1 {
		t.Fatal("the signature was written again")
	}
}

func TestNoSignatureClearsWarmblysAndLeavesGmailAlone(t *testing.T) {
	h := newHarness(t)
	mm := &memMailboxes{status: map[uuid.UUID]string{}}
	m := connectedSigned(t, h, mm, "acmeoutreach.com", graphkit.ProviderGoogle, MailboxSignature{})
	if got := mm.signatures[*m.EmailAccountID]; len(got) != 1 || got[0] != "" {
		t.Fatalf("warmbly signatures = %q", got)
	}
	if len(h.delegator.signatures) != 0 {
		t.Fatalf("an empty signature reached Gmail: %q", h.delegator.signatures)
	}
	if m.SignatureSetAt == nil || m.SignatureProviderAt == nil {
		t.Fatalf("stamps = %v %v", m.SignatureSetAt, m.SignatureProviderAt)
	}
}

func TestMicrosoftMailboxSignsInWarmblyOnly(t *testing.T) {
	h := newHarness(t)
	mm := &memMailboxes{status: map[uuid.UUID]string{}}
	m := connectedSigned(t, h, mm, "acmeoutreach.com", graphkit.ProviderMicrosoft)
	if len(mm.signatures[*m.EmailAccountID]) != 1 {
		t.Fatalf("warmbly signatures = %q", mm.signatures)
	}
	if len(h.delegator.signatures) != 0 {
		t.Fatalf("Microsoft has no signature API, yet one was sent: %q", h.delegator.signatures)
	}
	if m.SignatureProviderAt == nil {
		t.Fatal("nothing is left to set at Microsoft, so the provider half is done")
	}
}

func TestReconcilerRetriesAGmailSignatureWithoutRewritingWarmblys(t *testing.T) {
	h := newHarness(t)
	h.delegator.signatureFail = 1
	mm := &memMailboxes{status: map[uuid.UUID]string{}}
	m := connectedSigned(t, h, mm, "acmeoutreach.com", graphkit.ProviderGoogle)
	if len(h.delegator.signatures) != 0 || m.SignatureSetAt == nil || m.SignatureProviderAt != nil {
		t.Fatalf("after the failed set: gmail %q, stamps %v %v", h.delegator.signatures, m.SignatureSetAt, m.SignatureProviderAt)
	}
	h.clock.advance(signatureRetryAfter + time.Minute)
	if err := h.s.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.delegator.signatures) != 1 {
		t.Fatalf("the reconciler did not retry Gmail: %q", h.delegator.signatures)
	}
	if got := mm.signatures[*m.EmailAccountID]; len(got) != 1 {
		t.Fatalf("Warmbly's copy was written %d times", len(got))
	}
	if fresh := h.repo.mailboxes[m.ID]; fresh.SignatureProviderAt == nil {
		t.Fatal("the retried set was not recorded")
	}
}

func TestPurchasedMailboxStartsColdSendingOnTheGraduationRamp(t *testing.T) {
	h := newHarness(t)
	mm := &memMailboxes{status: map[uuid.UUID]string{}}
	connectedSigned(t, h, mm, "acmeoutreach.com", graphkit.ProviderGoogle)
	if len(mm.lifecycle) != 1 || mm.lifecycle[0] != "start" {
		t.Fatalf("warmup actions on connect = %q, want start", mm.lifecycle)
	}
	// Starting warmup is what puts a mailbox under the graduation ceiling: its first cold day is far below the cap.
	now := time.Now()
	if got := warmupramp.ColdCeiling(0, time.Time{}, nil, now, config.CampaignLimitDefault); got >= config.CampaignLimitDefault || got > 20 {
		t.Fatalf("a fresh mailbox's first cold day = %d", got)
	}
}
