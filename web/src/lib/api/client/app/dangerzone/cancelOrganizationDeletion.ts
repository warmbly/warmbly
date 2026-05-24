import Request from "../../Request";

export interface CancelDeletionPayload {
    reason?: string;
}

export default async function cancelOrganizationDeletion(
    data: CancelDeletionPayload = {},
): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/organization/current/danger-zone/delete`,
        data,
        authorization: true,
    });
}
