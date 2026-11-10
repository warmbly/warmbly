// The organization's custom forms domain. Mirrors models.FormsDomainStatus,
// and is shaped like the mailbox tracking-domain status on purpose.
import type { DNSRecord } from "../emails/SendingDomain";

export default interface FormsDomainStatus {
    forms_domain: string;
    forms_domain_verified: boolean;
    forms_domain_verified_at?: Date;
    /** The value to put in the CNAME: this install's forms host. */
    cname_target: string;
    /** The ownership TXT record a new verification needs beside the CNAME. */
    txt_name?: string;
    txt_value?: string;
    /** verified | unset | no_target | not_found | wrong_target | proof_missing | lookup_error | pending */
    status: string;
    message: string;
    /** What DNS actually returned, so a typo is visible. */
    observed?: string;
    forms_host_unresolvable: boolean;
    /** TXT records the edge in front of this instance still needs before it serves the domain over HTTPS. */
    edge_records?: DNSRecord[];
}
