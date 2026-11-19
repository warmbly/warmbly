import type { CreditTransactionsPage } from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

/** Ledger categories the server filters on; omitted means every line. */
export type CreditTransactionCategory = "store";

export default async function getCreditTransactions(
    limit = 25,
    cursor?: string,
    category?: CreditTransactionCategory,
): Promise<CreditTransactionsPage> {
    const params = new URLSearchParams({ limit: String(limit) });
    if (cursor) params.set("cursor", cursor);
    if (category) params.set("category", category);
    return await Request<CreditTransactionsPage>({
        method: "GET",
        url: `/subscription/credits/transactions?${params.toString()}`,
        authorization: true,
    });
}
