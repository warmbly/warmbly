import Request from "../../Request";

export default async function removeMember(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/organization/members/${id}`,
        authorization: true,
    })
}
