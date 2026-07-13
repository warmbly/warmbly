import type { CreditTransactionsPage } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

export default async function getCreditTransactions(
    limit = 25,
    cursor?: string,
): Promise<CreditTransactionsPage> {
    const params = new URLSearchParams({ limit: String(limit) });
    if (cursor) params.set("cursor", cursor);
    return await Request<CreditTransactionsPage>({
        method: "GET",
        url: `/subscription/credits/transactions?${params.toString()}`,
        authorization: true,
    });
}
