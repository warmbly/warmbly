import type ScheduledDeletion from "@/lib/api/models/app/dangerzone/ScheduledDeletion";
import type { ScheduleDeletionPayload } from "./scheduleOrganizationDeletion";
import Request from "../../Request";

export default async function scheduleAccountDeletion(
    data: ScheduleDeletionPayload,
): Promise<ScheduledDeletion> {
    return await Request<ScheduledDeletion>({
        method: "POST",
        url: `/me/danger-zone/delete`,
        data,
        authorization: true,
    });
}
