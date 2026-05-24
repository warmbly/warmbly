import type ScheduledDeletion from "@/lib/api/models/app/dangerzone/ScheduledDeletion";
import Request from "../../Request";

export interface ScheduleDeletionPayload {
    confirmation: string;
    reason?: string;
}

export default async function scheduleOrganizationDeletion(
    data: ScheduleDeletionPayload,
): Promise<ScheduledDeletion> {
    return await Request<ScheduledDeletion>({
        method: "POST",
        url: `/organization/current/danger-zone/delete`,
        data,
        authorization: true,
    });
}
