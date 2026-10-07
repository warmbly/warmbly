package mailhdr

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestReceivedEvidenceNeverTrustsAuthenticationResults(t *testing.T) {
	e := ReceivedEvidence(map[string][]string{
		"AUTHENTICATION-RESULTS": {"trusted.receiver; spf=pass dkim=pass dmarc=pass"},
		"From":                   {"Sender <local@example.test>"},
		"Return-Path":            {"<bounce@route.test>"},
		"DKIM-Signature":         {"v=1; d=example.test; s=mail; h=From:List-Unsubscribe:List-Unsubscribe-Post; b=unverified"},
		"List-Unsubscribe":       {"<https://example.test/opaque-token>"},
		"List-Unsubscribe-Post":  {"List-Unsubscribe=One-Click"},
	}, "gmail_api")
	if !e.AuthenticationResultsPresent || e.Trust != "unverified_headers" || e.SPF != "unknown" || e.DKIM != "unknown" || e.DMARC != "unknown" || e.Alignment != "unknown" || e.TLS != "unknown" || e.OneClickCompliance != "unknown" {
		t.Fatalf("header claims acquired trust: %+v", e)
	}
	if e.FromDomain != "example.test" || e.EnvelopeDomain != "route.test" || e.SigningDomain != "example.test" || e.Selector != "mail" || !e.OneClickHeaders || !e.OneClickSigningClaim {
		t.Fatalf("diagnostic claims lost: %+v", e)
	}
	serialized, err := json.Marshal(models.EvidenceFromFlags([]string{e.Flag()}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "opaque-token") {
		t.Fatal("unsubscribe token retained")
	}
}

func TestReceivedEvidenceRejectsMalformedClaims(t *testing.T) {
	for _, signature := range []string{"d=one.test; d=two.test; s=mail", "d=bad/domain; s=bad selector", strings.Repeat("x", 8193)} {
		e := ReceivedEvidence(map[string][]string{"DKIM-Signature": {signature}}, "imap_headers")
		if e.SigningDomain != "" || e.Selector != "" {
			t.Fatalf("unsafe claim accepted: %+v", e)
		}
	}
	for _, list := range []string{"<http://example.test/unsubscribe>", "https://example.test/no-brackets", "<https://user:password@example.test/>", "<https://example.test/a>, <https://example.test/b>"} {
		e := ReceivedEvidence(map[string][]string{"List-Unsubscribe": {list}, "List-Unsubscribe-Post": {"List-Unsubscribe=One-Click"}}, "graph_api")
		if e.OneClickHeaders {
			t.Fatal("invalid one-click header pair accepted")
		}
	}
	e := ReceivedEvidence(map[string][]string{"From": {"one@example.test", "two@example.test"}, "List-Unsubscribe": {"<https://example.test/>"}, "List-Unsubscribe-Post": {"wrong"}}, "graph_api")
	if e.FromDomain != "" || e.OneClickHeaders {
		t.Fatal("ambiguous claims accepted")
	}
}
