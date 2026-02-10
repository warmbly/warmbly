import type Pipeline from "@/lib/api/models/app/crm/Pipeline";
import Request from "../../../Request";

export default async function getPipeline(id: string): Promise<Pipeline> {
    return await Request<Pipeline>({
        method: "GET",
        url: `/crm/pipelines/${id}`,
        authorization: true,
    })
}
