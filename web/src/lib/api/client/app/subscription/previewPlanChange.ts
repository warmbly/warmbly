import Request from "../../Request";

export default async function previewPlanChange(planId: string): Promise<{ proration_amount: number; next_billing_date: Date }> {
    const params = new URLSearchParams();
    params.append("plan_id", planId);
    const url = `/subscription/preview-change?${params.toString()}`;

    return await Request<{ proration_amount: number; next_billing_date: Date }>({
        method: "GET",
        url,
        authorization: true,
    })
}
