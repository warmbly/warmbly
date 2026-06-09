import Request from "../../Request";

export default async function deleteAutomation(id: string): Promise<{ deleted: boolean }> {
    return await Request<{ deleted: boolean }>({
        method: "DELETE",
        url: `/automations/${id}`,
        authorization: true,
    });
}
