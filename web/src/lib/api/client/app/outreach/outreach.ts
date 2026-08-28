import type { OutreachSettings } from "@/lib/api/models/app/outreach/OutreachSettings";
import Request from "../../Request";

export async function getOutreachSettings(): Promise<OutreachSettings> {
    return await Request<OutreachSettings>({
        method: "GET",
        url: "/outreach/settings",
        authorization: true,
    });
}

// Replaces the whole settings object; callers send the blocks they are not
// editing back unchanged. Returns 204, so there is no body to parse.
export async function updateOutreachSettings(settings: OutreachSettings): Promise<void> {
    await Request({
        method: "PATCH",
        url: "/outreach/settings",
        data: { settings },
        authorization: true,
    });
}
