import type { CancelDeletionPayload } from "./cancelOrganizationDeletion";
import Request from "../../Request";

export default async function cancelAccountDeletion(
    data: CancelDeletionPayload = {},
): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/me/danger-zone/delete`,
        data,
        authorization: true,
    });
}
