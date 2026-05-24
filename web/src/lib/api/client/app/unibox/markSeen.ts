import Request from "../../Request";

export default async function markSeen(data: { ids: string[] }): Promise<void> {
    return await Request<void>({
        method: "PATCH",
        url: `/unibox/seen`,
        data,
        authorization: true,
    })
}
