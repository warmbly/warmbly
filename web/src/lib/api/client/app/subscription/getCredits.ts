import type { CreditBalance } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

export default async function getCredits(): Promise<CreditBalance> {
    return await Request<CreditBalance>({
        method: "GET",
        url: `/subscription/credits`,
        authorization: true,
    });
}
