// Inbox list + search client. Calls GET /unibox with the filter params
// the backend supports today: from, subject, unseen, since, until,
// cursor, limit. Empty fields are dropped so the URL stays readable
// in the network tab.

import Request from "../../Request";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

interface UniboxListResponse {
    data: {
        id: string;
        email_id: string;
        thread_id: string;
        subject: string;
        snippet: string;
        internal_date: string;
        seen: boolean;
    }[];
    pagination: {
        has_more: boolean;
        next_cursor: string | null;
    };
}

function isoDay(d: Date): string {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
}

export default async function searchIncoming(
    p: UniboxSearchParams = {},
): Promise<UniboxListResponse> {
    const usp = new URLSearchParams();
    // The server's free-text matcher is subject. If the frontend wants
    // body matching too, that's a server change — surfacing the param
    // here in case the user passed something.
    if (p.query) usp.set("subject", p.query);
    if (p.from) usp.set("from", p.from);
    if (p.accountIds && p.accountIds.length > 0) {
        // Backend accepts comma-separated email_ids and falls back to a
        // single email_id for legacy callers; we always use the multi form.
        usp.set("email_ids", p.accountIds.join(","));
    }
    if (p.unseen) usp.set("unseen", "true");
    if (p.since) usp.set("since", isoDay(p.since));
    if (p.until) usp.set("until", isoDay(p.until));
    if (p.cursor) usp.set("cursor", p.cursor);
    if (p.limit) usp.set("limit", String(p.limit));

    const qs = usp.toString();
    return Request<UniboxListResponse>({
        method: "GET",
        url: `/unibox${qs ? `?${qs}` : ""}`,
        authorization: true,
    });
}
