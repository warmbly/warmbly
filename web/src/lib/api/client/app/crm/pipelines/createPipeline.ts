import type Pipeline from "@/lib/api/models/app/crm/Pipeline";
import Request from "../../../Request";

export default async function createPipeline(data: { name: string; description?: string }): Promise<Pipeline> {
    return await Request<Pipeline>({
        method: "POST",
        url: `/crm/pipelines`,
        data,
        authorization: true,
    })
}
