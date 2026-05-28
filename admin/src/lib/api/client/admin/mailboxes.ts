// /admin/mailboxes — cross-org mailbox triage.

import { Request } from "@/lib/api/client";
import type {
    AdminMailboxSearch,
    AdminMailboxesResult,
} from "@/lib/api/models/admin";

export function searchMailboxes(
    params: AdminMailboxSearch = {},
): Promise<AdminMailboxesResult> {
    const usp = new URLSearchParams();
    if (params.q) usp.set("q", params.q);
    if (params.status) usp.set("status", params.status);
    if (params.provider) usp.set("provider", params.provider);
    if (params.cursor) usp.set("cursor", params.cursor);
    if (params.limit != null) usp.set("limit", String(params.limit));
    const s = usp.toString();
    return Request({
        method: "GET",
        url: `/admin/mailboxes${s ? `?${s}` : ""}`,
        authorization: true,
    });
}
