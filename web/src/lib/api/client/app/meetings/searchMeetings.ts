import type { MeetingsPage, MeetingsSearch } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

// Meetings page list. Offset-paginated (the backend uses offset so nullable
// scheduled_for sorts don't drop rows), so the page param is the next offset.
export default async function searchMeetings(
    filters: MeetingsSearch,
    offset = 0,
    limit = 50,
): Promise<MeetingsPage> {
    const qs = new URLSearchParams();
    if (filters.timeframe) qs.set("timeframe", filters.timeframe);
    if (filters.status) qs.set("status", filters.status);
    if (filters.q) qs.set("q", filters.q);
    if (offset) qs.set("offset", String(offset));
    if (limit) qs.set("limit", String(limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    return await Request<MeetingsPage>({
        method: "GET",
        url: `/meetings${suffix}`,
        authorization: true,
    });
}
