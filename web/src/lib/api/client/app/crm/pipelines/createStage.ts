import type { Stage } from "@/lib/api/models/app/crm/Pipeline";
import Request from "../../../Request";

export default async function createStage(pipelineId: string, data: { name: string; position: number; color?: string }): Promise<Stage> {
    return await Request<Stage>({
        method: "POST",
        url: `/crm/pipelines/${pipelineId}/stages`,
        data,
        authorization: true,
    })
}
