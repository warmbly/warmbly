package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/opsnotify"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Response codes the store answers with. Every GraphKit refusal maps to one
// of these with Warmbly's own message; its codes and wording never pass through.
const (
	CodeDisabled              = "store_disabled"
	CodeSalesPaused           = "store_sales_paused"
	CodeUnavailable           = "store_unavailable"
	CodeBusy                  = "rate_limited"
	CodeRegistrantRequired    = "registrant_required"
	CodeQuoteNotFound         = "quote_not_found"
	CodePriceChanged          = "price_changed"
	CodePaymentMethodRequired = "payment_method_required"
	CodePaymentProcessing     = "payment_processing"
	CodeChargeInProgress      = "charge_in_progress"
	CodeInsufficientBalance   = "insufficient_balance"
	CodeDomainUnavailable     = "domain_unavailable"
	CodeDomainPremium         = "domain_premium"
	CodeTLDUnsupported        = "tld_unsupported"
	CodeDomainNotReady        = "domain_not_ready"
	CodeDomainNotRenewable    = "domain_not_renewable"
	CodeRenewalNotDue         = "renewal_not_due"
	CodeRenewalWindowClosed   = "renewal_window_closed"
	CodeRenewalInProgress     = "renewal_in_progress"
	CodeTransferLocked        = "transfer_locked"
	CodeProviderMixed         = "provider_mixed"
	// CodeProviderUnavailable refuses mailboxes from a provider that is not offered right now.
	CodeProviderUnavailable   = "provider_unavailable"
	CodeAddressTaken          = "mailbox_exists"
	CodeAddressInvalid        = "local_part_invalid"
	CodeMailboxLimit          = "mailbox_limit"
	CodeMailboxNotCancellable = "mailbox_not_cancellable"
	CodeForwardingInvalid     = "forwarding_url_invalid"
	CodeInvalidItems          = "store_items_invalid"
	// CodeRegisteredElsewhere refuses a registrar operation on a domain the workspace connected.
	CodeRegisteredElsewhere = "domain_registered_elsewhere"
	// CodeRegisteredHere refuses removing a domain bought here; it leaves by transfer.
	CodeRegisteredHere    = "domain_registered_here"
	CodeDomainInUse       = "domain_in_use"
	CodeDNSRecordConflict = "dns_record_conflict"
	CodeDNSRecordManaged  = "dns_record_managed"
	CodeDNSRecordInvalid  = "dns_record_invalid"
	// CodeDNSNotHosted refuses a record change on a domain whose DNS the customer's provider holds.
	CodeDNSNotHosted       = "dns_not_hosted"
	CodeSignInUnavailable  = "sign_in_unavailable"
	CodeStoreStateConflict = "store_state_conflict"
	CodePurchasedMailbox   = "purchased_mailbox"
)

var errDisabled = errx.NewWithIdentifier(errx.NotFound, CodeDisabled,
	"Buying domains and mailboxes is not set up on this instance. An instance admin turns it on under Setup and health > Integrations in the admin panel.")

// NotConfiguredMessage answers an operator's store request while no GraphKit API key is configured.
const NotConfiguredMessage = "The store is not configured on this instance. Add a GraphKit API key under Setup and health > Integrations, or set GRAPHKIT_API_KEY."

var errSalesPaused = errx.NewWithIdentifier(errx.ServiceUnavailable, CodeSalesPaused,
	"Buying new domains and mailboxes is paused for now and will be available again soon. What you already bought is not affected.")

var errManaged = errx.NewWithIdentifier(errx.Forbidden, CodeManagedByPartner, "This workspace's domains and mailboxes are bought through its partner.")

func registeredElsewhere() *errx.Error {
	return errx.NewWithIdentifier(errx.Conflict, CodeRegisteredElsewhere, "This domain is registered elsewhere. Renew or transfer it at its registrar.")
}

// domainUnavailable says why a domain cannot be added, by GraphKit's reason.
func domainUnavailable(reason string) *errx.Error {
	msg := "This domain is no longer available. Pick another one."
	switch reason {
	case "already_added":
		msg = "This domain is already in your workspace."
	case "registered":
		msg = "This domain was bought through Warmbly by another workspace."
	case "connected_elsewhere":
		msg = "Another workspace is already connecting this domain."
	}
	e := errx.NewWithIdentifier(errx.Conflict, CodeDomainUnavailable, msg)
	switch reason {
	case "already_added", "registered", "connected_elsewhere":
		e.Reason = reason
	}
	return e
}

// stateConflict is GraphKit's "not in this state" refusal in the operation's terms.
func stateConflict(op string) *errx.Error {
	switch {
	case strings.HasPrefix(op, "dns."):
		return errx.NewWithIdentifier(errx.Conflict, CodeDNSNotHosted, "This domain's DNS is at its own provider. Change the record there.")
	case op == "domain.remove":
		return errx.NewWithIdentifier(errx.Conflict, CodeDomainInUse, "This domain still has mailboxes. Cancel them before removing it.")
	case op == "mailbox.resume":
		return errx.NewWithIdentifier(errx.Conflict, CodeMailboxNotCancellable, "This mailbox's cancellation already took effect.")
	case op == "mailbox.cancel":
		return errx.NewWithIdentifier(errx.Conflict, CodeMailboxNotCancellable, "This mailbox can't be cancelled now.")
	}
	return errx.NewWithIdentifier(errx.Conflict, CodeStoreStateConflict, "This can't be done right now. Refresh and try again.")
}

// alertEvery is how often one partner-account problem reaches the operator.
const alertEvery = time.Hour

// alertIfAccountLevel tells the operator when GraphKit refuses this
// instance's partner account itself; workspaces only see the store unavailable.
func (s *Service) alertIfAccountLevel(err error) {
	ge, ok := graphkit.AsError(err)
	if !ok || !ge.AccountLevel() {
		return
	}
	s.mu.Lock()
	if s.alerted == nil {
		s.alerted = map[string]time.Time{}
	}
	last, seen := s.alerted[ge.Code]
	fire := !seen || s.now().Sub(last) >= alertEvery
	if fire {
		s.alerted[ge.Code] = s.now()
	}
	s.mu.Unlock()
	if !fire {
		return
	}
	summary := "GraphKit refused this instance's partner account, so buying domains and mailboxes is unavailable to every workspace."
	switch ge.Code {
	case graphkit.CodePartnerSuspended:
		summary = "GraphKit suspended this instance's partner account. Every workspace sees buying domains and mailboxes as unavailable until GraphKit lifts it."
	case graphkit.CodeUnauthorized:
		summary = "GraphKit refused GRAPHKIT_API_KEY (revoked, unknown or the wrong mode). Buying domains and mailboxes is unavailable until the key is replaced."
	case graphkit.CodePartnerBalanceInsufficient:
		summary = "The GraphKit partner balance is too low for orders and renewals. Workspaces were not charged for refused orders; top the balance up."
	}
	log.Error().Str("upstream_code", ge.Code).Msg("store: " + summary)
	s.recordAlert(ge.Code, "Buying domains and mailboxes is unavailable")
	if s.Ops != nil {
		s.Ops.NotifyOperator(opsnotify.EventStoreUnavailable, "Buying domains and mailboxes is unavailable", summary,
			map[string]string{"code": ge.Code, "request_id": ge.RequestID})
	}
}

// providerUnavailable refuses mailboxes from a provider that cannot be ordered right now; nothing is charged.
func providerUnavailable(provider string) *errx.Error {
	name := "These mailboxes"
	switch provider {
	case graphkit.ProviderMicrosoft:
		name = "Microsoft 365 mailboxes"
	case graphkit.ProviderGoogle:
		name = "Google Workspace mailboxes"
	}
	return errx.NewWithIdentifier(errx.Unprocessable, CodeProviderUnavailable, name+" aren't available right now. Choose another provider for these mailboxes.")
}

func unavailable() *errx.Error {
	e := errx.NewWithIdentifier(errx.ServiceUnavailable, CodeUnavailable, "Buying domains and mailboxes is unavailable right now. Try again in a few minutes.")
	e.Public = true
	return e
}

// upstream maps a GraphKit failure to a Warmbly error and logs the real cause.
// op names the call ("dns.add", "domain.remove", ...), since GraphKit's
// generic refusals mean different things to the workspace per operation.
func (s *Service) upstream(_ context.Context, op string, err error) *errx.Error {
	logUpstream(op, err)
	ge, ok := graphkit.AsError(err)
	if !ok {
		return unavailable()
	}
	if ge.AccountLevel() {
		s.alertIfAccountLevel(err)
		return unavailable()
	}
	if p := ge.UnavailableProvider(); p != "" {
		return providerUnavailable(p)
	}
	id := func(code errx.Code, ident, msg string) *errx.Error { return errx.NewWithIdentifier(code, ident, msg) }
	dnsOp := strings.HasPrefix(op, "dns.")
	switch ge.Code {
	case graphkit.CodeContactInvalid:
		return contactErr(ge.ContactFields())
	case graphkit.CodeDomainUnavailable:
		return domainUnavailable(ge.DetailReason())
	case graphkit.CodeNotRegisteredHere:
		return registeredElsewhere()
	case graphkit.CodeDNSRecordConflict:
		if !dnsOp {
			return id(errx.Conflict, CodeDNSRecordConflict, "Records on this domain clash with the mail records it needs. Remove them first.")
		}
		return id(errx.Conflict, CodeDNSRecordConflict, "This record clashes with one Warmbly keeps for mail. Change its name or type.")
	case graphkit.CodeDNSRecordManaged:
		return id(errx.Conflict, CodeDNSRecordManaged, "Warmbly keeps this record for mail delivery, so it can't be changed here.")
	case graphkit.CodeCredentialsUnavailable:
		return id(errx.Conflict, CodeSignInUnavailable, "This mailbox has no sign-in to show.")
	case graphkit.CodeStateConflict:
		return stateConflict(op)
	case graphkit.CodeInvalidRequest:
		switch {
		case dnsOp:
			return id(errx.BadRequest, CodeDNSRecordInvalid, "Check the record: its name, value and TTL.")
		case op == "quote", op == "domain.connect":
			return itemsErr("Check the domains and mailboxes in your cart.")
		}
	case graphkit.CodeDomainPremium:
		return id(errx.Unprocessable, CodeDomainPremium, "Premium domains can't be bought here. Pick another one.")
	case graphkit.CodeTLDUnsupported:
		return id(errx.Unprocessable, CodeTLDUnsupported, "Domains with this ending can't be bought here.")
	case graphkit.CodeProviderMixed:
		return id(errx.Unprocessable, CodeProviderMixed, "All mailboxes on a domain use the same provider.")
	case graphkit.CodeMailboxExists:
		return id(errx.Conflict, CodeAddressTaken, "One of these addresses already exists on the domain.")
	case graphkit.CodeMailboxLimit:
		return id(errx.Unprocessable, CodeMailboxLimit, "A domain holds at most 5 mailboxes.")
	case graphkit.CodeLocalPartInvalid:
		return id(errx.BadRequest, CodeAddressInvalid, "One of these addresses can't be used. Use letters, digits, dots, dashes or underscores.")
	case graphkit.CodeDomainNotReady:
		return id(errx.Conflict, CodeDomainNotReady, "This domain is still being set up. Try again once it's active.")
	case graphkit.CodeRenewalNotDue:
		return id(errx.Unprocessable, CodeRenewalNotDue, "This domain can't be renewed yet.")
	case graphkit.CodeRenewalWindowClosed:
		return id(errx.Unprocessable, CodeRenewalWindowClosed, "This domain can no longer be renewed.")
	case graphkit.CodeRenewalInProgress:
		return id(errx.Conflict, CodeRenewalInProgress, "This domain is already being renewed.")
	case graphkit.CodeTransferLocked:
		return id(errx.Conflict, CodeTransferLocked, "This domain can't be transferred right now. Try again later.")
	case graphkit.CodeNotFound:
		return errx.ErrNotFound
	case graphkit.CodeRateLimited:
		e := id(errx.TooManyRequests, CodeBusy, "Too many requests. Try again in a moment.")
		if ge.RetryAfter > 0 {
			e.RetryAfter = int(ge.RetryAfter.Seconds())
		}
		return e
	}
	return unavailable()
}

// PriceChangedError says the total moved since the buyer saw it; Quote is the fresh one.
type PriceChangedError struct {
	Quote *Quote
}

func (e *PriceChangedError) Error() string { return "store: price changed" }

// PaymentMethodError means the balance is short and there is no card to top it up from.
type PaymentMethodError struct {
	ShortMicros int64
}

func (e *PaymentMethodError) Error() string { return "store: payment method required" }

// CardError is a card refusal while topping up at checkout; Code is the refusal's code.
type CardError struct {
	Code string
}

func (e *CardError) Error() string { return "store: card declined: " + e.Code }

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

var (
	queryRe = regexp.MustCompile(`^[\p{L}\p{N} .-]{1,63}$`)
	tldRe   = regexp.MustCompile(`^[a-z]{2,24}$`)
	// recordNameRe is a relative DNS name: labels of letters, digits, hyphens and underscores, or a leading wildcard.
	recordNameRe = regexp.MustCompile(`^(\*\.)?([a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?)(\.[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?)*$`)
)

func cleanQuery(q string) (string, *errx.Error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if !queryRe.MatchString(q) {
		return "", errx.NewWithIdentifier(errx.BadRequest, "invalid_query", "Enter a word or a domain name to search for.")
	}
	return q, nil
}

func cleanTLDs(in []string) []string {
	var out []string
	for _, t := range in {
		for _, p := range strings.Split(t, ",") {
			p = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p), "."))
			if tldRe.MatchString(p) && len(out) < 10 {
				out = append(out, p)
			}
		}
	}
	return out
}

// cleanForwarding accepts an empty value or an absolute https URL.
func cleanForwarding(raw string) (string, *errx.Error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(raw) > 2048 || u.User != nil {
		return "", errx.NewWithIdentifier(errx.BadRequest, CodeForwardingInvalid, "Enter a full https:// address for the domain to forward to.")
	}
	return u.String(), nil
}

// PurchasedMailboxPaused refuses removing a purchased mailbox while GraphKit is not configured, when it cannot be cancelled either.
func PurchasedMailboxPaused() *errx.Error {
	return errx.NewWithIdentifier(errx.Forbidden, CodePurchasedMailbox,
		"This mailbox was bought through Warmbly and is still billed by its provider. It can be cancelled once your instance operator reconnects the store.")
}

// PurchasedMailboxLocked refuses removing or reconnecting a purchased mailbox the workspace still pays for.
func PurchasedMailboxLocked() *errx.Error {
	return errx.NewWithIdentifier(errx.Forbidden, CodePurchasedMailbox,
		"This mailbox was bought through Warmbly and stays billed while you have it. Cancel it under Subscription in its settings to stop it at the end of the paid month, or switch it off to pause it.")
}
