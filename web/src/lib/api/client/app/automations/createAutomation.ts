import type { Automation, AutomationWrite } from "@/lib/api/models/app/automations/Automation";
import Request from "../../Request";

export default async function createAutomation(w: AutomationWrite): Promise<{ automation: Automation }> {
    return await Request<{ automation: Automation }>({
        method: "POST",
        url: "/automations",
        data: w,
        authorization: true,
    });
}
