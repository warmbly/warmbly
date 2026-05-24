import Request from "../../../Request";

export default async function deletePipeline(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/crm/pipelines/${id}`,
        authorization: true,
    })
}
