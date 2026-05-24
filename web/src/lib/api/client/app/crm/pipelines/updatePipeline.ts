import type Pipeline from "@/lib/api/models/app/crm/Pipeline";
import Request from "../../../Request";

export default async function updatePipeline(id: string, data: Partial<Pipeline>): Promise<Pipeline> {
    return await Request<Pipeline>({
        method: "PATCH",
        url: `/crm/pipelines/${id}`,
        data,
        authorization: true,
    })
}
