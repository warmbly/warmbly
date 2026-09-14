// The mailbox's sending identity: which addresses its provider will let it
// send as, which one is in use, and where the stored signature came from.
// Read through /emails/:id/identity, like sync state and sending behaviour,
// rather than on the mailbox object: the list is only ever needed in the
// drawer.
export interface SendAsIdentity {
    email: string;
    name: string;
    is_primary: boolean;
    is_default: boolean;
    /** Unverified addresses are listed but cannot be selected: the provider
     *  refuses to send as one. */
    verified: boolean;
}

export default interface SendIdentity {
    /** False for Outlook and SMTP/IMAP mailboxes, which expose no send-as
     *  list. The refresh endpoint refuses them. */
    supported: boolean;
    provider: string;
    mailbox_email: string;
    /** Empty means the mailbox's own address. */
    send_as_email: string;
    identities: SendAsIdentity[];
    synced_at?: string | null;
    signature_source: "manual" | "provider";
    signature_imported_at?: string | null;
}
