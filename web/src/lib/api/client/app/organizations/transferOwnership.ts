import Request from "../../Request";

export default async function transferOwnership(newOwnerUserId: string): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/organization/transfer-ownership`,
        data: { new_owner_user_id: newOwnerUserId },
        authorization: true,
    })
}
