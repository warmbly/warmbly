import Request from "../../Request";

export interface CreateCreditCheckoutInput {
    pack: string;
    success_url: string;
    cancel_url: string;
}

export default async function createCreditCheckout(
    data: CreateCreditCheckoutInput,
): Promise<{ session_id: string; checkout_url: string }> {
    return await Request<{ session_id: string; checkout_url: string }>({
        method: "POST",
        url: `/subscription/credits/checkout`,
        data,
        authorization: true,
    });
}
