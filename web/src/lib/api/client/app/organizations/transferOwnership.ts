import Request from "../../Request";

export default async function transferOwnership(data: { user_id: string }): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/organization/transfer-ownership`,
        data,
        authorization: true,
    })
}
