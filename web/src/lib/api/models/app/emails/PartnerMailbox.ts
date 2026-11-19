/** What a workspace's partner lets one mailbox do; members read it, the partner sets it. */
export type PartnerEnablement = "off" | "warmup_only" | "sending_warmup";

export default interface PartnerMailbox {
    email_account_id: string;
    enablement: PartnerEnablement;
    /** The partner attached it: it is removed or reconnected there, not here. */
    managed: boolean;
}
