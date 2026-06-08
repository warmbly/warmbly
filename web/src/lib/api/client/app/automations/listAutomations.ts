import type { Automation } from "@/lib/api/models/app/automations/Automation";
import Request from "../../Request";

export default async function listAutomations(): Promise<{ automations: Automation[] }> {
    return await Request<{ automations: Automation[] }>({
        method: "GET",
        url: "/automations",
        authorization: true,
    });
}
