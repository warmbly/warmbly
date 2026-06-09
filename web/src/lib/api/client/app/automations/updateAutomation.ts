import type { Automation, AutomationWrite } from "@/lib/api/models/app/automations/Automation";
import Request from "../../Request";

export default async function updateAutomation(id: string, w: AutomationWrite): Promise<{ automation: Automation }> {
    return await Request<{ automation: Automation }>({
        method: "PATCH",
        url: `/automations/${id}`,
        data: w,
        authorization: true,
    });
}
