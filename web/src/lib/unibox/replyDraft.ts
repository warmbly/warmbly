export type ReplyMode = "reply" | "forward";

export interface ReplySeed {
    to: string[];
    cc: string[];
    bcc: string[];
    subject: string;
    body: string;
    /** The sending mailbox when the draft chose one; absent means the thread's own. */
    email_account_id?: string;
}

export function replyDraftKey(userId: string, orgId: string, threadId: string, messageId: string, mode: ReplyMode): string {
    return `warmbly-reply-draft:${JSON.stringify([userId, orgId, threadId, messageId, mode])}`;
}

function recipients(value: unknown): string[] {
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

export function loadReplyDraft(key: string): ReplySeed | null {
    try {
        const raw = localStorage.getItem(key);
        if (!raw) return null;
        const d = JSON.parse(raw) as Partial<ReplySeed>;
        if (typeof d?.body !== "string") return null;
        return {
            to: recipients(d.to),
            cc: recipients(d.cc),
            bcc: recipients(d.bcc),
            subject: typeof d.subject === "string" ? d.subject : "",
            body: d.body,
            ...(typeof d.email_account_id === "string" && d.email_account_id
                ? { email_account_id: d.email_account_id }
                : {}),
        };
    } catch {
        return null;
    }
}

// Compare against composer defaults; empty recipients and subjects can be intentional.
export function saveReplyDraft(key: string, draft: ReplySeed): boolean {
    try {
        localStorage.setItem(key, JSON.stringify(draft));
        return true;
    } catch {
        return false;
    }
}

export function clearReplyDraft(key: string, expected?: ReplySeed): boolean {
    try {
        if (expected && localStorage.getItem(key) !== JSON.stringify(expected)) return true;
        localStorage.removeItem(key);
        return true;
    } catch {
        return false;
    }
}
