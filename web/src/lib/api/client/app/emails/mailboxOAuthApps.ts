import Request from "../../Request";
import type {
    MailboxOAuthApp,
    MailboxOAuthAppInput,
    MailboxOAuthAppsResult,
    MailboxOAuthProvider,
} from "@/lib/api/models/app/emails/MailboxOAuthApp";

export async function listMailboxOAuthApps(): Promise<MailboxOAuthAppsResult> {
    return await Request<MailboxOAuthAppsResult>({ method: "GET", url: `/emails/oauth-apps`, authorization: true });
}

export async function saveMailboxOAuthApp(provider: MailboxOAuthProvider, data: MailboxOAuthAppInput): Promise<MailboxOAuthApp> {
    return await Request<MailboxOAuthApp>({ method: "PUT", url: `/emails/oauth-apps/${provider}`, data, authorization: true });
}

export async function deleteMailboxOAuthApp(provider: MailboxOAuthProvider): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/emails/oauth-apps/${provider}`, authorization: true });
}
