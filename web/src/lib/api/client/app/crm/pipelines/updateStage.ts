import type { Stage } from "@/lib/api/models/app/crm/Pipeline";
import Request from "../../../Request";

export default async function updateStage(pipelineId: string, stageId: string, data: Partial<Stage>): Promise<Stage> {
    return await Request<Stage>({
        method: "PATCH",
        url: `/crm/pipelines/${pipelineId}/stages/${stageId}`,
        data,
        authorization: true,
    })
}
