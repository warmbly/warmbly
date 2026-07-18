import type { AISpendSettings } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

export default async function getCreditSettings(): Promise<AISpendSettings> {
    return await Request<AISpendSettings>({
        method: "GET",
        url: `/subscription/credits/settings`,
        authorization: true,
    });
}
