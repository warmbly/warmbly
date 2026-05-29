import Request from "../../Request";

export interface ChangePlanInput {
    plan_id: string;
    // Optional discount/promo code applied to the plan change.
    discount_code?: string;
}

export default async function changePlan(data: ChangePlanInput): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/subscription/change-plan`,
        data,
        authorization: true,
    });
}
