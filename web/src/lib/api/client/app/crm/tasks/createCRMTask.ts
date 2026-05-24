import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import Request from "../../../Request";

export default async function createCRMTask(data: Partial<CRMTask>): Promise<CRMTask> {
    return await Request<CRMTask>({
        method: "POST",
        url: `/crm/tasks`,
        data,
        authorization: true,
    })
}
