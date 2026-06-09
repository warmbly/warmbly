import type { AutomationRun } from "@/lib/api/models/app/automations/Automation";
import Request from "../../Request";

export default async function listAutomationRuns(id: string): Promise<{ runs: AutomationRun[] }> {
    return await Request<{ runs: AutomationRun[] }>({
        method: "GET",
        url: `/automations/${id}/runs`,
        authorization: true,
    });
}
