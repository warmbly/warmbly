/** A mailbox provider a workspace can bring its own OAuth client for. */
export type MailboxOAuthProvider = "gmail" | "outlook";

/** A workspace's own Google or Microsoft OAuth client. The secret never comes back. */
export interface MailboxOAuthApp {
    id: string;
    organization_id: string;
    provider: MailboxOAuthProvider;
    client_id: string;
    app_name: string;
    /** The callback to register with the provider. */
    redirect_url: string;
    /** True when the workspace forwards the callback from its own domain. */
    custom_redirect: boolean;
    /** False after a workspace import without credentials: save the secret again. */
    has_secret: boolean;
    created_by?: string;
    created_at: string;
    updated_at: string;
    /** Mailboxes whose sign-in this client issued. */
    mailboxes: number;
}

/** Saving a client; an empty client_secret keeps the stored one for the same client ID. */
export interface MailboxOAuthAppInput {
    client_id: string;
    client_secret: string;
    app_name: string;
    redirect_url: string;
}

export interface MailboxOAuthAppsResult {
    data: MailboxOAuthApp[];
    /** Warmbly's own callback per provider, to register unless you forward from your own domain. */
    callbacks: Partial<Record<MailboxOAuthProvider, string>>;
}
