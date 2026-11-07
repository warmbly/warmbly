// /emails/grants: an administrator's grant over a Google Workspace domain or a Microsoft 365 tenant.
import Request from "@/lib/api/client/Request";
import type { MailboxImport } from "@/lib/api/models/app/emails/MailboxImport";
import type {
    DirectoryUser,
    DomainGrant,
    GrantConfig,
    GrantConnectRequest,
    ListResponse,
    SigninMigrationList,
} from "@/lib/api/models/app/emails/MailboxSources";

export async function getGrantConfig(): Promise<GrantConfig> {
    return await Request<GrantConfig>({ method: "GET", url: "/emails/grants/config", authorization: true });
}

export async function listGrants(): Promise<ListResponse<DomainGrant>> {
    return await Request<ListResponse<DomainGrant>>({ method: "GET", url: "/emails/grants", authorization: true });
}

export async function getGrant(id: string): Promise<DomainGrant> {
    return await Request<DomainGrant>({ method: "GET", url: `/emails/grants/${id}`, authorization: true });
}

// The admin consent URL a Global Administrator opens, and the state its callback echoes back.
export async function startMicrosoftGrant(): Promise<{ url: string; state: string }> {
    return await Request<{ url: string; state: string }>({ method: "POST", url: "/emails/grants/microsoft/start", authorization: true });
}

export async function finishMicrosoftGrant(body: { state: string; code: string }): Promise<DomainGrant> {
    return await Request<DomainGrant>({
        method: "POST",
        url: "/emails/grants/microsoft/finish",
        data: body,
        authorization: true,
        timeout: 60_000,
    });
}

// Verifies again; on recovery the grant's stopped mailboxes restart.
export async function checkGrant(id: string): Promise<DomainGrant> {
    return await Request<DomainGrant>({ method: "POST", url: `/emails/grants/${id}/check`, authorization: true, timeout: 60_000 });
}

// The grant's mailboxes stop.
export async function deleteGrant(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/emails/grants/${id}`, authorization: true });
}

export async function listGrantUsers(id: string): Promise<ListResponse<DirectoryUser>> {
    return await Request<ListResponse<DirectoryUser>>({
        method: "GET",
        url: `/emails/grants/${id}/users`,
        authorization: true,
        timeout: 60_000,
    });
}

export async function connectGrantUsers(id: string, body: GrantConnectRequest): Promise<MailboxImport> {
    return await Request<MailboxImport>({
        method: "POST",
        url: `/emails/grants/${id}/connect`,
        data: body,
        authorization: true,
        timeout: 60_000,
    });
}

// The workspace's mailboxes still on per-mailbox Google sign-in, by domain.
export async function getSigninMigration(): Promise<SigninMigrationList> {
    return await Request<SigninMigrationList>({ method: "GET", url: "/emails/grants/migration", authorization: true });
}
