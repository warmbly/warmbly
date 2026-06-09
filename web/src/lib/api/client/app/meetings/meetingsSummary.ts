import type { MeetingsSummary } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function meetingsSummary(): Promise<MeetingsSummary> {
    return await Request<MeetingsSummary>({
        method: "GET",
        url: "/meetings/summary",
        authorization: true,
    });
}
