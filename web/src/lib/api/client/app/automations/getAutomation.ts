import type { Automation } from "@/lib/api/models/app/automations/Automation";
import Request from "../../Request";

export default async function getAutomation(id: string): Promise<{ automation: Automation }> {
    return await Request<{ automation: Automation }>({
        method: "GET",
        url: `/automations/${id}`,
        authorization: true,
    });
}
