// Compose drafts: autosaved, per-user working copies of unsent emails. The
// client generates the draft id and PUTs the whole draft on a debounce, so
// autosave is idempotent and retries are safe.

import Request from "../../Request";

export interface ComposeDraft {
    id: string;
    email_account_id?: string | null;
    to: string[];
    cc: string[];
    bcc: string[];
    subject: string;
    body: string;
    updated_at: string;
    created_at: string;
}

export interface ComposeDraftSaveInput {
    email_account_id?: string;
    to: string[];
    cc: string[];
    bcc: string[];
    subject: string;
    body: string;
}

export async function listComposeDrafts(): Promise<ComposeDraft[]> {
    const res = await Request<{ data: ComposeDraft[] }>({
        method: "GET",
        url: "/unibox/drafts",
        authorization: true,
    });
    return res.data ?? [];
}

export async function saveComposeDraft(
    id: string,
    data: ComposeDraftSaveInput,
): Promise<void> {
    await Request({
        method: "PUT",
        url: `/unibox/drafts/${id}`,
        data,
        authorization: true,
    });
}

export async function deleteComposeDraft(id: string): Promise<void> {
    await Request({
        method: "DELETE",
        url: `/unibox/drafts/${id}`,
        authorization: true,
    });
}
