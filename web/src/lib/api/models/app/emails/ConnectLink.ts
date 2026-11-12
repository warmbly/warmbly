// A client connect link: a client opens it and connects their own mailboxes into the workspace.

/** The ways a client may connect: one mailbox at a time, or many from an inbox vendor account. */
export type ConnectLinkProvider = "gmail" | "outlook" | "smtp_imap" | "vendor";
export type ConnectLinkStatus = "active" | "expired" | "revoked" | "exhausted";

export interface ConnectLink {
    id: string;
    organization_id: string;
    created_by: string | null;
    label: string;
    allowed_providers: ConnectLinkProvider[];
    max_mailboxes: number;
    mailboxes_connected: number;
    tag_ids: string[];
    warmup: boolean;
    expires_at: string;
    revoked_at: string | null;
    created_at: string;
    last_used_at: string | null;
    status: ConnectLinkStatus;
}

export interface ConnectLinkList {
    data: ConnectLink[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

export interface NewConnectLink {
    label: string;
    providers: ConnectLinkProvider[];
    max_mailboxes: number;
    expires_in_days: number;
    tag_ids: string[];
    warmup: boolean;
}

/** The URL is in this response only; the server keeps a hash of it. */
export interface CreatedConnectLink {
    link: ConnectLink;
    url: string;
}

export interface ConnectLinkUse {
    id: string;
    link_id: string;
    email_account_id: string | null;
    email: string;
    provider: string;
    connected_at: string;
}

export const CONNECT_LINK_PROVIDERS: { id: ConnectLinkProvider; label: string }[] = [
    { id: "gmail", label: "Google" },
    { id: "outlook", label: "Microsoft" },
    { id: "smtp_imap", label: "IMAP / SMTP" },
    { id: "vendor", label: "Inbox vendor account" },
];
