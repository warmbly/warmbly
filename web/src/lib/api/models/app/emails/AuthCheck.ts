// Result of GET /emails/:id/auth-check — a live SPF/DKIM/DMARC validation of
// the mailbox's sending domain. Surfaced in the mailbox detail drawer so an
// owner can confirm their authentication is aligned before sending cold mail.
export default interface AuthCheck {
    domain: string;
    spf_found: boolean;
    dkim_found: boolean;
    dmarc_found: boolean;
    dmarc_policy: string;
    all_aligned: boolean;
    summary: string;
    dkim_selectors?: string[];
    spf_record?: string;
}
