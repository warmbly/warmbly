package mailhost

import (
	"context"
	"net"
	"strings"
	"time"
)

const txtTimeout = 3 * time.Second

// RecipientResolver is the DNS surface Recipient needs; *net.Resolver satisfies it.
type RecipientResolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Recipient works out who hosts a recipient domain's inboxes from DNS alone.
// It fetches nothing over HTTPS and asks no third party, because the domain is
// a lead's. ok is false when a lookup failed transiently and the answer should
// be asked again later; a domain that has no mail comes back Unknown and ok.
func Recipient(ctx context.Context, r RecipientResolver, domain string) (Host, bool) {
	norm := NormalizeDomain(domain)
	if norm == "" {
		return Unknown, true
	}
	if h := recipientKnown(norm); h != Unknown {
		return h, true
	}
	if r == nil {
		r = net.DefaultResolver
	}
	d := &Detector{resolver: mxOnly{r}}
	mx, err := d.lookupMX(ctx, norm)
	if err != nil {
		return Unknown, notFound(err)
	}
	if len(mx) == 0 {
		return Unknown, true
	}
	for _, host := range mx {
		if h, _ := classify(host); h != Unknown {
			return Refine(h, norm), true
		}
	}
	// A filtering gateway or a relay fronts the MX; SPF usually still names the mailbox host.
	if h := fromSPF(ctx, r, norm); h != Unknown {
		return h, true
	}
	return Other, true
}

// KnownDomain names the host of a consumer mail domain, or of an address's
// domain, from the built-in list with no DNS; Unknown for anything else.
func KnownDomain(s string) Host {
	return recipientKnown(NormalizeDomain(s))
}

// recipientKnown is knownDomain plus Microsoft 365 tenant domains, which are
// Microsoft's mail but not a consumer service, so SharedProvider leaves them out.
func recipientKnown(domain string) Host {
	if h, ok := knownDomain(domain); ok {
		return h
	}
	if strings.HasSuffix(domain, ".onmicrosoft.com") {
		return Microsoft365
	}
	return Unknown
}

// ESPFamily is h's family as campaign ESP matching and contacts.esp_provider
// name it: "gmail", "outlook", "other", or "" when h is Unknown.
func ESPFamily(h Host) string {
	switch {
	case h == Unknown:
		return ""
	case h.Google():
		return "gmail"
	case h.Microsoft():
		return "outlook"
	}
	return "other"
}

// fromSPF reads the provider out of the include: mechanisms of domain's SPF record.
func fromSPF(ctx context.Context, r RecipientResolver, domain string) Host {
	ctx, cancel := context.WithTimeout(ctx, txtTimeout)
	defer cancel()
	txts, err := r.LookupTXT(ctx, domain)
	if err != nil {
		return Unknown
	}
	for _, t := range txts {
		fields := strings.Fields(strings.ToLower(strings.TrimSpace(t)))
		if len(fields) == 0 || fields[0] != "v=spf1" {
			continue
		}
		for _, f := range fields[1:] {
			f = strings.TrimLeft(f, "+~?")
			inc, ok := strings.CutPrefix(f, "include:")
			if !ok {
				continue
			}
			if h, _ := classify(inc); h.Google() || h.Microsoft() || h == Zoho {
				return Refine(h, domain)
			}
		}
	}
	return Unknown
}

// mxOnly adapts a RecipientResolver to lookupMX, which never asks for SRV.
type mxOnly struct{ RecipientResolver }

func (mxOnly) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, &net.DNSError{Err: "not supported", IsNotFound: true}
}
