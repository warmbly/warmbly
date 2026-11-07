import type { AISpendSettings } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

// Absent or null limits are removed.
export type UpdateCreditSettingsBody = Omit<AISpendSettings, "auto_reload_enabled">;

export default async function updateCreditSettings(body: UpdateCreditSettingsBody): Promise<AISpendSettings> {
    return await Request<AISpendSettings>({
        method: "PATCH",
        url: `/subscription/credits/settings`,
        authorization: true,
        data: body,
    });
}
