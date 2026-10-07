// /cloud-link/* — the self-hosted instance's link to Warmbly Cloud.

import Request from "@/lib/api/client/Request";
import type {
    CloudLinkMailboxRow,
    CloudLinkOAuthStart,
    CloudLinkPendingConnect,
    CloudLinkPollResult,
    CloudLinkStatus,
    PoolLinkWorkspaceMailbox,
    DiagnosticParticipation,
} from "@/lib/api/models/app/cloudlink/CloudLink";
import type Inbox from "@/lib/api/models/app/emails/Inbox";

export async function getCloudLinkStatus(): Promise<CloudLinkStatus> {
    return await Request<CloudLinkStatus>({ method: "GET", url: "/cloud-link", authorization: true });
}

export async function startCloudLinkConnect(cloudUrl?: string): Promise<CloudLinkPendingConnect> {
    return await Request<CloudLinkPendingConnect>({
        method: "POST",
        url: "/cloud-link/connect",
        data: { cloud_url: cloudUrl ?? "" },
        authorization: true,
    });
}

export async function pollCloudLinkConnect(): Promise<CloudLinkPollResult> {
    return await Request<CloudLinkPollResult>({ method: "POST", url: "/cloud-link/connect/poll", authorization: true });
}

export async function disconnectCloudLink(legacy = false): Promise<void> {
    await Request<void>({ method: "DELETE", url: "/cloud-link", params: legacy ? { legacy: true } : undefined, authorization: true });
}

export async function listCloudLinkMailboxes(): Promise<CloudLinkMailboxRow[]> {
    const res = await Request<{ data: CloudLinkMailboxRow[] }>({ method: "GET", url: "/cloud-link/mailboxes", authorization: true });
    return res.data ?? [];
}

export async function enrollCloudLinkMailbox(id: string): Promise<CloudLinkMailboxRow> {
    return await Request<CloudLinkMailboxRow>({ method: "POST", url: `/cloud-link/mailboxes/${id}/enroll`, authorization: true });
}

export async function unenrollCloudLinkMailbox(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/cloud-link/mailboxes/${id}/enroll`, authorization: true });
}

export async function setCloudLinkMailboxLifecycle(id: string, action: "pause" | "resume"): Promise<CloudLinkMailboxRow> {
    return await Request<CloudLinkMailboxRow>({ method: "POST", url: `/cloud-link/mailboxes/${id}/${action}`, authorization: true });
}

export async function setCloudLinkParticipation(id: string, participation: DiagnosticParticipation): Promise<CloudLinkMailboxRow> {
    return await Request<CloudLinkMailboxRow>({ method: "PATCH", url: `/cloud-link/mailboxes/${id}/participation`, data: participation, authorization: true });
}

// Cloud-managed mailboxes: Google/Microsoft sign-in through Warmbly Cloud's own
// OAuth app, and adoption of mailboxes connected directly on the workspace.

export async function startCloudOAuth(provider: "gmail" | "outlook"): Promise<CloudLinkOAuthStart> {
    return await Request<CloudLinkOAuthStart>({ method: "POST", url: "/cloud-link/oauth/start", data: { provider }, authorization: true });
}

export async function finishCloudOAuth(session: string): Promise<Inbox> {
    return await Request<Inbox>({ method: "POST", url: "/cloud-link/oauth/finish", data: { session }, authorization: true });
}

export async function listCloudWorkspaceMailboxes(): Promise<PoolLinkWorkspaceMailbox[]> {
    const res = await Request<{ data: PoolLinkWorkspaceMailbox[] }>({ method: "GET", url: "/cloud-link/workspace-mailboxes", authorization: true });
    return res.data ?? [];
}

export async function adoptCloudMailbox(id: string): Promise<Inbox> {
    return await Request<Inbox>({ method: "POST", url: `/cloud-link/workspace-mailboxes/${id}/adopt`, authorization: true });
}
