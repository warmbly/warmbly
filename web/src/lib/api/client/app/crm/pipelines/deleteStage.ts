import Request from "../../../Request";

export default async function deleteStage(pipelineId: string, stageId: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/crm/pipelines/${pipelineId}/stages/${stageId}`,
        authorization: true,
    })
}
