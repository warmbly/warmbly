import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import Request from "../../../Request";

export default async function updateCRMTask(id: string, data: Partial<CRMTask>): Promise<CRMTask> {
    return await Request<CRMTask>({
        method: "PATCH",
        url: `/crm/tasks/${id}`,
        data,
        authorization: true,
    })
}
