// Result of the mailbox auth check: a live SPF/DKIM/DMARC validation of the
// sending domain. GET /emails/:id/auth-check reports it; POST records it, which
// is what clears the cold-send and warmup gate after a DNS fix.
//
// Surfaced in the mailbox detail drawer so an owner can confirm their
// authentication is aligned before sending cold mail.
export default interface AuthCheck {
    domain: string;
    spf_found: boolean;
    dkim_found: boolean;
    dmarc_found: boolean;
    dmarc_policy: string;
    /**
     * Where the DMARC record was found, and whether it belongs to a parent
     * domain. A dedicated sending subdomain with no record of its own is
     * covered by its organizational domain's policy (RFC 7489), so this says
     * "acme.com" for a mail.acme.com mailbox that inherits.
     */
    dmarc_domain?: string;
    dmarc_inherited: boolean;
    all_aligned: boolean;
    summary: string;
    dkim_selectors?: string[];
    spf_record?: string;
    /** True when DNS could not answer, so the result is unknown, not failing. */
    lookup_error: boolean;
}
