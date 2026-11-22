package store

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

const hostile = "https://evil.example/login GraphKit"

func neutral(t *testing.T, where, s string) {
	t.Helper()
	low := strings.ToLower(s)
	for _, bad := range []string{"graphkit", "evil.example", "http"} {
		if strings.Contains(low, bad) {
			t.Errorf("%s carries %q: %q", where, bad, s)
		}
	}
}

// What a member is told about a refused purchase is Warmbly's own words, whatever the upstream sent.
func TestMemberStoreErrorsCarryNoUpstreamText(t *testing.T) {
	s := New(Deps{Repo: newMemRepo()})
	details, _ := json.Marshal(map[string]any{"fields": []string{"phone", hostile}, "reason": hostile})
	codes := []string{
		graphkit.CodeInvalidRequest, graphkit.CodeIdempotencyConflict, graphkit.CodeNotFound, graphkit.CodeAccountNotFound,
		graphkit.CodeDomainUnavailable, graphkit.CodeDomainPremium, graphkit.CodeTLDUnsupported, graphkit.CodeQuoteExpired,
		graphkit.CodeQuoteMismatch, graphkit.CodeContactInvalid, graphkit.CodeProviderMixed, graphkit.CodeMailboxExists,
		graphkit.CodeMailboxLimit, graphkit.CodeLocalPartInvalid, graphkit.CodeDomainNotReady, graphkit.CodeRenewalNotDue,
		graphkit.CodeRenewalWindowClosed, graphkit.CodeExpectedExpiryMismatch, graphkit.CodeTransferLocked,
		graphkit.CodePartnerBalanceInsufficient, graphkit.CodeUpstreamUnavailable, graphkit.CodeRateLimited, graphkit.CodeInternal,
		graphkit.CodeUnauthorized, graphkit.CodeProvisioningFailed, graphkit.CodeCredentialsUnavailable, graphkit.CodeDNSRecordConflict,
		graphkit.CodeDNSRecordManaged, graphkit.CodeRenewalInProgress, graphkit.CodeStateConflict, graphkit.CodeNotRegisteredHere,
		graphkit.CodePartnerSuspended, graphkit.CodeSendingUnavailable, graphkit.CodeTransport, hostile,
	}
	for _, op := range []string{"quote", "order", "registrant", "domain.connect", "domain.renew", "dns.add", "mailbox.cancel"} {
		for _, code := range codes {
			e := s.upstream(context.Background(), op, &graphkit.Error{Status: 400, Code: code, Message: hostile, Details: details})
			neutral(t, op+"/"+code+" message", e.Message)
			neutral(t, op+"/"+code+" reason", e.Reason)
			neutral(t, op+"/"+code+" identifier", e.Identifier)
		}
	}
	neutral(t, "store disabled", errDisabled.Message)
	neutral(t, "paused purchased mailbox", PurchasedMailboxPaused().Message)
}

func TestContactRefusalsNameOnlyKnownFields(t *testing.T) {
	e := NewContactError([]string{"country", hostile, "first_name", "fax"})
	if !reflect.DeepEqual(e.Fields, []string{"first_name", "country"}) {
		t.Fatalf("fields %v", e.Fields)
	}
	if got := e.Message(); got != "Check the domain owner's details: first name, country." {
		t.Fatalf("message %q", got)
	}
	unknown := NewContactError([]string{hostile, "fax"})
	if len(unknown.Fields) != 0 || unknown.Message() != "Check the domain owner's details and try again." {
		t.Fatalf("unknown fields: %v %q", unknown.Fields, unknown.Message())
	}
	neutral(t, "unknown fields", unknown.Error())

	// Through the upstream mapping, the reason the handler reads back is the known fields only.
	xe := contactErr([]string{"postal_code", hostile})
	if xe.Reason != "postal_code" || xe.Message != "Check the domain owner's details: postal code." {
		t.Fatalf("mapped: %q %q", xe.Reason, xe.Message)
	}

	// Every registrant field has a label, so a refusal of any of them is named.
	labelled := map[string]bool{}
	for _, f := range contactFields {
		labelled[f.key] = true
	}
	rt := reflect.TypeOf(Registrant{})
	for i := range rt.NumField() {
		if tag := rt.Field(i).Tag.Get("json"); !labelled[tag] {
			t.Errorf("registrant field %s has no label", tag)
		}
	}
	if len(contactFields) != rt.NumField() {
		t.Errorf("%d labels for %d registrant fields", len(contactFields), rt.NumField())
	}
}
