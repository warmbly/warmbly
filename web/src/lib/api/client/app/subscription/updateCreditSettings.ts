import type { AISpendSettings } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

export interface UpdateCreditSettingsBody {
    spend_limit_daily: number | null;
    spend_limit_weekly: number | null;
    spend_limit_monthly: number | null;
    low_balance_threshold: number;
    auto_topup_enabled: boolean;
    auto_topup_pack: string;
    auto_topup_threshold: number;
    auto_topup_max_per_month: number;
}

export default async function updateCreditSettings(body: UpdateCreditSettingsBody): Promise<AISpendSettings> {
    return await Request<AISpendSettings>({
        method: "PATCH",
        url: `/subscription/credits/settings`,
        authorization: true,
        data: body,
    });
}
