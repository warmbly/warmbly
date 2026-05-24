import Request from "../../../Request";

export default async function deleteCRMTask(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/crm/tasks/${id}`,
        authorization: true,
    })
}
