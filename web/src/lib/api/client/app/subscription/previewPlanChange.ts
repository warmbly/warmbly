import Request from "../../Request";

// proration_amount and amount_due come straight from Stripe, so they are in
// the currency's minor unit (cents for USD). Callers must not render them
// as-is.
export default async function previewPlanChange(planId: string): Promise<{
    proration_amount: number;
    amount_due?: number;
    currency?: string;
    next_billing_date: Date;
}> {
    const params = new URLSearchParams();
    params.append("plan_id", planId);
    const url = `/subscription/preview-change?${params.toString()}`;

    return await Request<{
        proration_amount: number;
        amount_due?: number;
        currency?: string;
        next_billing_date: Date;
    }>({
        method: "GET",
        url,
        authorization: true,
    })
}
