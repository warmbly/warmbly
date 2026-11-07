import Request from "../../Request";
import type { AppError } from "../../normalizeError";
import removeEmail from "./removeEmail";
import updateEmail from "./updateEmail";
import warmupLifecycle, { type WarmupAction } from "./warmupLifecycle";

export interface BulkMailboxResult {
    id: string;
    ok: boolean;
    code?: string;
    message?: string;
}

export interface BulkMailboxResponse {
    succeeded: number;
    failed: number;
    results: BulkMailboxResult[];
}

// The server takes at most this many ids per call.
const BULK_MAX_IDS = 500;

// A backend from before the bulk routes answers its router's bare 404, with no error code.
const missingRoute = (e: unknown) => (e as AppError | null)?.status === 404 && !(e as AppError).code;

// The single-mailbox routes, one request each, for a backend that has no bulk route yet.
async function oneByOne(ids: string[], apply: (id: string) => Promise<unknown>): Promise<BulkMailboxResponse> {
    const settled = await Promise.allSettled(ids.map(apply));
    const results = settled.map((r, i): BulkMailboxResult => {
        if (r.status === "fulfilled") return { id: ids[i], ok: true };
        const err = r.reason as AppError | null;
        return { id: ids[i], ok: false, code: err?.code, message: err?.message };
    });
    const failed = results.filter((r) => !r.ok).length;
    return { succeeded: results.length - failed, failed, results };
}

// One call per chunk, each under its own Idempotency-Key, so a retried chunk is not applied twice.
async function bulk(path: string, ids: string[], extra: Record<string, string>, single: (id: string) => Promise<unknown>): Promise<BulkMailboxResponse> {
    const out: BulkMailboxResponse = { succeeded: 0, failed: 0, results: [] };
    for (let i = 0; i < ids.length; i += BULK_MAX_IDS) {
        const chunk = ids.slice(i, i + BULK_MAX_IDS);
        let res: BulkMailboxResponse;
        try {
            res = await Request<BulkMailboxResponse>({
                method: "POST",
                url: `/emails/bulk/${path}`,
                data: { email_ids: chunk, ...extra },
                headers: { "Idempotency-Key": crypto.randomUUID() },
                authorization: true,
            });
        } catch (e) {
            if (!missingRoute(e)) throw e;
            res = await oneByOne(chunk, single);
        }
        out.succeeded += res.succeeded;
        out.failed += res.failed;
        out.results.push(...(res.results ?? []));
    }
    return out;
}

export const bulkRemoveEmails = (ids: string[]) => bulk("delete", ids, {}, removeEmail);

export const bulkWarmupEmails = (ids: string[], action: WarmupAction) =>
    bulk("warmup", ids, { action }, (id) => warmupLifecycle(id, action));

export const bulkSwitchEmails = (ids: string[], status: "active" | "inactive") =>
    bulk("status", ids, { status }, (id) => updateEmail(id, { status }));
