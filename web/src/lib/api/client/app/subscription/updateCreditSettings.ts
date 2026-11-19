import type { AISpendSettings } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

// Absent or null spend limits are removed; the AI overage fields are kept when absent.
export type UpdateCreditSettingsBody = Omit<AISpendSettings, "auto_reload_enabled">;

export default async function updateCreditSettings(body: UpdateCreditSettingsBody): Promise<AISpendSettings> {
    return await Request<AISpendSettings>({
        method: "PATCH",
        url: `/subscription/credits/settings`,
        authorization: true,
        data: body,
    });
}
