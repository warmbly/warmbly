package provisioning

import (
	"context"
	"net"
	"strings"
	"time"
)

const rdnsLookupTimeout = 5 * time.Second

// VerifyReverseDNS confirms a sending IP has a PTR record AND that the PTR name
// forward-resolves back to the same IP (forward-confirmed reverse DNS / FCrDNS).
// A missing or mismatched PTR is a classic mailbox-provider rejection cause, so
// this complements the set-side SetReverseDNS by verifying the record actually
// took and is self-consistent.
//
// Returns the PTR hostname (if any), whether FCrDNS holds, and any lookup error.
func VerifyReverseDNS(ctx context.Context, ip string) (ptr string, fcrdnsOK bool, err error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "", false, nil
	}

	resolver := &net.Resolver{}

	c1, cancel1 := context.WithTimeout(ctx, rdnsLookupTimeout)
	defer cancel1()
	names, err := resolver.LookupAddr(c1, ip)
	if err != nil || len(names) == 0 {
		return "", false, err
	}
	ptr = strings.TrimSuffix(names[0], ".")

	c2, cancel2 := context.WithTimeout(ctx, rdnsLookupTimeout)
	defer cancel2()
	addrs, ferr := resolver.LookupHost(c2, ptr)
	if ferr != nil {
		return ptr, false, ferr
	}
	for _, a := range addrs {
		if a == ip {
			return ptr, true, nil
		}
	}
	return ptr, false, nil
}
