import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import Request from "../../../Request";

export default async function getCRMTask(id: string): Promise<CRMTask> {
    return await Request<CRMTask>({
        method: "GET",
        url: `/crm/tasks/${id}`,
        authorization: true,
    })
}
