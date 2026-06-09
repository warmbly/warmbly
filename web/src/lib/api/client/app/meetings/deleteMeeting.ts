import Request from "../../Request";

export default async function deleteMeeting(id: string): Promise<{ deleted: boolean }> {
    return await Request<{ deleted: boolean }>({
        method: "DELETE",
        url: `/meetings/${id}`,
        authorization: true,
    });
}
